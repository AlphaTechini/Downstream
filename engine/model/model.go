// Package model holds the JSON documents the engine reads and writes:
// scan.json, journeys.json, notes.json and run.json. Every package in the
// engine exchanges data through these types, so field names here are the
// file formats.
package model

import "encoding/json"

// FormatVersion is written into every document the engine produces.
const FormatVersion = 1

// Scan is scan.json: what a diff changed and which routes it can reach.
type Scan struct {
	Version      int           `json:"version"`
	Base         string        `json:"base"`
	Head         string        `json:"head"`
	Root         string        `json:"root"`   // directory analysed, relative to the repo
	Method       string        `json:"method"` // one line describing the analysis and its limits
	ChangedFiles []ChangedFile `json:"changed_files"`
	ChangedFuncs []string      `json:"changed_funcs"`       // functions whose body or declarations they use overlap a hunk
	Routes       []Route       `json:"routes"`              // route table of the head build
	Changed      []Impact      `json:"changed"`             // routes whose handler call path reaches a changed function
	Affected     []Impact      `json:"affected"`            // routes that share a dependency with a changed route
	NewRoutes    []Route       `json:"new_routes"`          // head routes with no matching method and pattern on the base
	AppNotes     string        `json:"app_notes,omitempty"` // downstream.md from the base commit: test data and key pages
	Notes        []string      `json:"notes,omitempty"`
}

// ChangedFile is one file in the diff with its hunks.
type ChangedFile struct {
	Path  string `json:"path"`
	Hunks []Hunk `json:"hunks"`
}

// Hunk is a line range on the old and new side of a unified diff.
type Hunk struct {
	OldStart int    `json:"old_start"`
	OldLines int    `json:"old_lines"`
	NewStart int    `json:"new_start"`
	NewLines int    `json:"new_lines"`
	Text     string `json:"text,omitempty"` // the hunk body, capped in size
}

// Route is one registered HTTP route.
type Route struct {
	Method     string   `json:"method"` // "" when the pattern has no method
	Path       string   `json:"path"`   // ServeMux pattern path, e.g. /books/{id}
	Handler    string   `json:"handler"`
	Middleware []string `json:"middleware,omitempty"` // outermost first
	File       string   `json:"file"`
	Line       int      `json:"line"`
}

// Impact is a route in the blast radius and why it is there.
type Impact struct {
	Route
	Reasons      []string `json:"reasons"`
	ChangedFuncs []string `json:"changed_funcs,omitempty"` // changed functions on its call path
}

// JourneyFile is journeys.json, written by the model and validated by the engine.
type JourneyFile struct {
	Version  int       `json:"version"`
	Journeys []Journey `json:"journeys"`
}

// Journey is what one user does, in order.
type Journey struct {
	ID          string `json:"id"`             // [a-z0-9_]{1,40}
	Kind        string `json:"kind,omitempty"` // KindExisting (the default), KindNew or KindControl
	Goal        string `json:"goal"`
	WhyIncluded string `json:"why_included"`
	Steps       []Step `json:"steps"`
}

// Journey kinds. Existing and control journeys run on both builds and are
// compared. New journeys use routes the merge request adds, so they run on the
// head build only, in their own pass.
const (
	KindExisting = "existing" // covers a changed or affected route that exists on the base
	KindNew      = "new"      // covers a route only the head has
	KindControl  = "control"  // unrelated to the change; catches app-wide effects such as CPU contention
)

// IsNew reports whether j runs on the head build only.
func (j Journey) IsNew() bool { return j.Kind == KindNew }

// Step is one request. Strings in Path, Headers and Body may contain {{var}}
// placeholders filled from earlier Extract results or the built-in {{uniq}}.
type Step struct {
	Name         string            `json:"name"` // [a-z0-9_]{1,40}, unique within the journey
	Method       string            `json:"method"`
	Path         string            `json:"path"`
	Headers      map[string]string `json:"headers,omitempty"`
	Body         json.RawMessage   `json:"body,omitempty"`
	ExpectStatus int               `json:"expect_status"`
	Extract      map[string]string `json:"extract,omitempty"` // var name -> JSON pointer into the response body
}

// Notes is notes.json, written by the model: a likely cause and fix options
// per regression. The engine places the text; it never computes from it.
type Notes struct {
	Version int             `json:"version"`
	Items   map[string]Note `json:"items"` // keyed by Finding.ID
}

// Note is the model's text for one finding.
type Note struct {
	LikelyCause string   `json:"likely_cause"`
	FixOptions  []string `json:"fix_options"`
}

// Run is run.json: the three passes, discarded journeys and findings.
type Run struct {
	Version   int       `json:"version"`
	Base      string    `json:"base"`
	Head      string    `json:"head"`
	StartedAt string    `json:"started_at"` // RFC 3339
	EndedAt   string    `json:"ended_at"`
	Settings  Settings  `json:"settings"`
	Journeys  []Journey `json:"journeys"` // the journeys that were kept
	Discarded []Discard `json:"discarded"`
	Passes    []Pass    `json:"passes"` // in run order: B1, C, B2
	// NewFeature holds the new journeys, which run in pass C only.
	NewFeature  *NewFeaturePass `json:"new_feature,omitempty"`
	Findings    []Finding       `json:"findings"`
	NotMeasured []string        `json:"not_measured"`
	Error       *RunError       `json:"error,omitempty"` // set when the run stopped early
}

// Settings records how the run was configured.
type Settings struct {
	VUs             int        `json:"vus"`
	Iterations      int        `json:"iterations"` // journey iterations per VU in each pass
	DurationSeconds int        `json:"duration_seconds"`
	K6Version       string     `json:"k6_version"`
	Thresholds      Thresholds `json:"thresholds"`
}

// Thresholds are the classification rules in effect (see agentnotes.md).
type Thresholds struct {
	BandMultiplier    float64 `json:"band_multiplier"`     // regression needs delta > this * noise band
	MinRelative       float64 `json:"min_relative"`        // and delta >= this fraction of the worse baseline
	MinLatencyMS      float64 `json:"min_latency_ms"`      // and delta >= this for latency metrics
	MinMemoryMB       float64 `json:"min_memory_mb"`       // and delta >= this for memory
	MinCPUSeconds     float64 `json:"min_cpu_seconds"`     // and delta >= this for CPU time
	MaxBaselineSpread float64 `json:"max_baseline_spread"` // verdict withheld when |B1-B2| / min(B1,B2) exceeds this
}

// Discard is a journey dropped because it failed on the baseline.
type Discard struct {
	Journey string `json:"journey"`
	Step    string `json:"step"`
	Reason  string `json:"reason"`
}

// Pass is one start of one build with the journeys run against it.
type Pass struct {
	Label     string        `json:"label"` // B1, C or B2
	Commit    string        `json:"commit"`
	StartedAt string        `json:"started_at"`
	EndedAt   string        `json:"ended_at"`
	Process   ProcessStats  `json:"process"`
	Steps     []StepMetrics `json:"steps"`
}

// NewFeaturePass holds the new journeys and their step figures from pass C,
// where they ran alongside the compared journeys. There is no baseline, so the
// figures are reported as measured, not classified. Process is C's, so it
// includes all of C's traffic.
type NewFeaturePass struct {
	Journeys []Journey `json:"journeys"`
	Pass     Pass      `json:"pass"`
}

// ProcessStats are read from /proc for the app process during the k6 run.
type ProcessStats struct {
	CPUSeconds float64 `json:"cpu_seconds"` // user+system time consumed while k6 ran
	PeakRSSMB  float64 `json:"peak_rss_mb"` // VmHWM at the end of the pass
}

// StepMetrics are k6 figures for one step in one pass. Latencies are in ms.
type StepMetrics struct {
	Journey  string  `json:"journey"`
	Step     string  `json:"step"`
	Count    int     `json:"count"`
	Failures int     `json:"failures"`
	P50      float64 `json:"p50_ms"`
	P95      float64 `json:"p95_ms"`
	Mean     float64 `json:"mean_ms"`
}

// Classes of finding.
const (
	ClassRegression = "regression"
	ClassTradeOff   = "trade-off"
	ClassImproved   = "improved"
	ClassNoise      = "noise"
	ClassWithheld   = "withheld" // baseline runs disagreed too much to judge
	// ClassNotReached marks a latency figure for a step that recorded no
	// requests in at least one pass, so there is nothing to compare.
	ClassNotReached = "not-reached"
)

// Metric names used in findings.
const (
	MetricP95      = "p95_ms"
	MetricP50      = "p50_ms"
	MetricMean     = "mean_ms"
	MetricFailures = "failures"
	MetricCPU      = "cpu_seconds"
	MetricRSS      = "peak_rss_mb"
)

// Finding compares one metric across B1, C and B2.
type Finding struct {
	ID       string  `json:"id"`      // stable: <journey>.<step>.<metric>, or app.<metric>
	Journey  string  `json:"journey"` // "" for app-wide process metrics
	Step     string  `json:"step"`
	Metric   string  `json:"metric"`
	Unit     string  `json:"unit"` // ms, s, MB, count
	B1       float64 `json:"b1"`
	C        float64 `json:"c"`
	B2       float64 `json:"b2"`
	Band     float64 `json:"band"`      // |B1 - B2|
	Delta    float64 `json:"delta"`     // C minus the worse baseline (positive is worse)
	DeltaPct float64 `json:"delta_pct"` // Delta as a percentage of the worse baseline
	Class    string  `json:"class"`
	Reason   string  `json:"reason"` // which rule decided the class, in words
}

// RunError says where a run stopped and why.
type RunError struct {
	Stage   string `json:"stage"` // e.g. "build (head)", "validate", "pass C"
	Message string `json:"message"`
}

// Marker is the JSON inside the hidden <!-- downstream:v1 ... --> comment of an issue.
type Marker struct {
	Version    int      `json:"v"`
	MR         int      `json:"mr"`
	Base       string   `json:"base"`
	Head       string   `json:"head"`
	FindingID  string   `json:"finding"`
	Metric     string   `json:"metric"`
	Step       string   `json:"step"`
	Journey    Journey  `json:"journey"`
	B1         float64  `json:"b1"`
	C          float64  `json:"c"`
	B2         float64  `json:"b2"`
	Band       float64  `json:"band"`
	FixOptions []string `json:"fix_options"`
}
