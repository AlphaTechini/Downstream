// Package history turns one run into a small, numbers-only record for the
// Downstream memory service, and compounds the records of merge requests that
// were merged into the current base into a trend.
//
// Absolute figures differ between sandbox sessions (the same branch measured
// 5.2 ms and 10.4 ms of CPU per journey in two sessions), so a record keeps each
// run's own controlled change: the candidate against the worse of its two
// baselines, measured on one machine in one session. Compounding those changes
// across merged merge requests shows a slow creep that no single merge request
// trips.
package history

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"time"

	"downstream/engine/model"
)

// Version is the record format.
const Version = 1

// Window is how many merged merge requests the trend looks back over.
const Window = 10

// CreepLimit is the compounded rise, including the current merge request, at
// which a metric is flagged as creeping.
const CreepLimit = 0.25

var (
	shaRe     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	projectRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,254}$`)
)

// Metric is one gated app-wide metric of a run.
type Metric struct {
	Label  string  `json:"label"`
	Unit   string  `json:"unit"`
	Base   float64 `json:"base"`   // the worse baseline
	C      float64 `json:"c"`      // the candidate
	Change float64 `json:"change"` // (C - base) / base
}

// Entry is one run's record. It holds no code, no diff and no route names.
type Entry struct {
	Version  int               `json:"v"`
	Project  string            `json:"project"`
	MR       int               `json:"mr"`
	Base     string            `json:"base"`
	Head     string            `json:"head"`
	Recorded string            `json:"recorded"` // RFC 3339, set by the service
	Metrics  map[string]Metric `json:"metrics"`
}

// Validate checks an entry's shape; the service calls it on every write.
func (e Entry) Validate() error {
	switch {
	case e.Version != Version:
		return fmt.Errorf("record version %d is not supported", e.Version)
	case !projectRe.MatchString(e.Project):
		return errors.New("project must be a GitLab project path")
	case e.MR < 1:
		return errors.New("mr must be a merge request number")
	case !shaRe.MatchString(e.Base) || !shaRe.MatchString(e.Head):
		return errors.New("base and head must be 40-character lowercase SHAs")
	case len(e.Metrics) == 0 || len(e.Metrics) > 40:
		return errors.New("metrics must hold 1 to 40 entries")
	}
	for k, m := range e.Metrics {
		if len(k) > 80 || len(m.Label) > 80 || len(m.Unit) > 20 {
			return fmt.Errorf("metric %q: name, label or unit too long", k)
		}
		for _, v := range []float64{m.Base, m.C, m.Change} {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return fmt.Errorf("metric %q: values must be finite", k)
			}
		}
	}
	return nil
}

// Summarize builds the record for a run: every gated app-wide metric that was
// compared (withheld and unmeasured ones are left out) with a non-zero baseline.
func Summarize(run model.Run, project string, mr int) (Entry, error) {
	e := Entry{Version: Version, Project: project, MR: mr, Base: run.Base, Head: run.Head, Metrics: map[string]Metric{}}
	for _, f := range run.Findings {
		if f.Journey != "" || !f.Gated {
			continue
		}
		if f.Class != model.ClassRegression && f.Class != model.ClassImproved && f.Class != model.ClassNoise {
			continue
		}
		base := math.Max(f.B1, f.B2)
		if base <= 0 {
			continue
		}
		e.Metrics[f.ID] = Metric{Label: f.Metric, Unit: f.Unit, Base: base, C: f.C, Change: (f.C - base) / base}
	}
	if err := e.Validate(); err != nil {
		return Entry{}, err
	}
	return e, nil
}

// Line is one metric of the trend.
type Line struct {
	ID       string    `json:"id"`
	Label    string    `json:"label"`
	Merged   int       `json:"merged"`    // merged merge requests that measured this metric
	MRs      []int     `json:"mrs"`       // their numbers, oldest first
	Before   float64   `json:"before"`    // compounded change of the merged ones
	This     float64   `json:"this"`      // this merge request's change
	IfMerged float64   `json:"if_merged"` // compounded including this one
	Creeping bool      `json:"creeping"`  // IfMerged reaches CreepLimit with at least one merged MR
	Changes  []float64 `json:"changes"`   // the merged ones' changes, oldest first
}

// Trend is trend.json.
type Trend struct {
	Version int     `json:"v"`
	Status  string  `json:"status"` // "ok", or "unavailable" with Reason
	Reason  string  `json:"reason,omitempty"`
	Project string  `json:"project"`
	Window  int     `json:"window"`
	Limit   float64 `json:"limit"`
	Lines   []Line  `json:"lines"`
	// Service is the memory service's URL, for the report's link.
	Service string `json:"service,omitempty"`
}

// Compute compounds the merged records with the current one. isMerged reports
// whether a recorded head is an ancestor of the current base; records whose
// head is not, and older records of the same merge request, are ignored.
func Compute(current Entry, prior []Entry, isMerged func(head string) bool) Trend {
	t := Trend{Version: Version, Status: "ok", Project: current.Project, Window: Window, Limit: CreepLimit}
	// The newest merged record of each merge request is the one that was merged.
	latest := map[int]Entry{}
	for _, p := range prior {
		if p.Project != current.Project || p.MR == current.MR || p.Head == current.Head {
			continue
		}
		if q, ok := latest[p.MR]; ok && !recordedAfter(p, q) {
			continue
		}
		if isMerged(p.Head) {
			latest[p.MR] = p
		}
	}
	merged := make([]Entry, 0, len(latest))
	for _, p := range latest {
		merged = append(merged, p)
	}
	sort.Slice(merged, func(i, k int) bool { return recordedAfter(merged[k], merged[i]) })
	if len(merged) > Window {
		merged = merged[len(merged)-Window:]
	}

	ids := make([]string, 0, len(current.Metrics))
	for id := range current.Metrics {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		cur := current.Metrics[id]
		l := Line{ID: id, Label: cur.Label, This: cur.Change, MRs: []int{}, Changes: []float64{}}
		before := 1.0
		for _, p := range merged {
			if m, ok := p.Metrics[id]; ok {
				before *= 1 + m.Change
				l.Merged++
				l.MRs = append(l.MRs, p.MR)
				l.Changes = append(l.Changes, m.Change)
			}
		}
		l.Before = before - 1
		l.IfMerged = before*(1+cur.Change) - 1
		l.Creeping = l.Merged > 0 && l.IfMerged >= CreepLimit
		t.Lines = append(t.Lines, l)
	}
	return t
}

func recordedAfter(a, b Entry) bool {
	ta, errA := time.Parse(time.RFC3339Nano, a.Recorded)
	tb, errB := time.Parse(time.RFC3339Nano, b.Recorded)
	if errA != nil || errB != nil {
		return a.Recorded > b.Recorded
	}
	return ta.After(tb)
}
