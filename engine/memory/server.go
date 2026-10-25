package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"time"

	"downstream/engine/history"
)

const maxBody = 64 << 10

// Server is the HTTP API and dashboard.
type Server struct {
	Store     Store
	Allowed   []string      // project paths accepted for writes and shown on the dashboard
	Retention time.Duration // records older than this are deleted on the next write
	// CommitExists reports whether head is a commit of project, so a record can
	// only be written for a real commit of an allowed project.
	CommitExists func(ctx context.Context, project, head string) (bool, error)
	Log          *slog.Logger
	Now          func() time.Time
}

// Handler routes the API and the dashboard.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/runs", s.record)
	mux.HandleFunc("GET /v1/runs", s.list)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "ok\n") })
	mux.HandleFunc("GET /{$}", s.dashboard)
	return mux
}

// recordResponse is what the flow gets back: the stored record and the
// project's earlier records, newest first, for the engine to compound.
type recordResponse struct {
	Recorded history.Entry   `json:"recorded"`
	Prior    []history.Entry `json:"prior"`
}

func (s *Server) record(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var e history.Entry
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		s.fail(w, http.StatusBadRequest, "body is not a run record: "+err.Error())
		return
	}
	if err := e.Validate(); err != nil {
		s.fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if !slices.Contains(s.Allowed, e.Project) {
		s.Log.Warn("run refused: project not opted in", "project", e.Project, "mr", e.MR)
		s.fail(w, http.StatusForbidden, "project "+e.Project+" is not opted in on this instance")
		return
	}
	ok, err := s.CommitExists(ctx, e.Project, e.Head)
	if err != nil {
		s.fail(w, http.StatusBadGateway, "could not check the commit on GitLab: "+err.Error())
		return
	}
	if !ok {
		s.Log.Warn("run refused: head commit not in project", "project", e.Project, "mr", e.MR, "head", e.Head)
		s.fail(w, http.StatusForbidden, "head "+e.Head+" is not a commit of "+e.Project)
		return
	}
	prior, err := s.Store.List(ctx, e.Project)
	if err != nil {
		s.fail(w, http.StatusBadGateway, "reading history: "+err.Error())
		return
	}
	e.Recorded = s.Now().UTC().Format(time.RFC3339Nano)
	if err := s.Store.Add(ctx, e); err != nil {
		s.fail(w, http.StatusBadGateway, "storing the record: "+err.Error())
		return
	}
	pruned, err := s.Store.Prune(ctx, e.Project, s.Now().Add(-s.Retention))
	if err != nil {
		s.Log.Warn("pruning old records failed", "project", e.Project, "error", err.Error())
	}
	cpu, hasCPU := e.Metrics["app.cpu_ms_per_journey"]
	attrs := []any{"project", e.Project, "mr", e.MR, "head", e.Head[:12], "metrics", len(e.Metrics), "prior_records", len(prior), "pruned", pruned}
	if hasCPU {
		attrs = append(attrs, "cpu_ms_per_journey_change_pct", round1(cpu.Change*100))
	}
	s.Log.Info(fmt.Sprintf("run recorded for !%d", e.MR), attrs...)
	writeJSON(w, http.StatusOK, recordResponse{Recorded: e, Prior: nonNil(prior)})
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("project")
	if !slices.Contains(s.Allowed, p) {
		s.fail(w, http.StatusNotFound, "project not opted in")
		return
	}
	es, err := s.Store.List(r.Context(), p)
	if err != nil {
		s.fail(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": p, "runs": nonNil(es)})
}

func (s *Server) fail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func nonNil(es []history.Entry) []history.Entry {
	if es == nil {
		return []history.Entry{}
	}
	return es
}

func round1(v float64) float64 { return float64(int64(v*10+0.5*sign(v))) / 10 }

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

// GitLabCommitExists checks a commit through GitLab's public API, so it works
// for public projects without a token.
func GitLabCommitExists(api string, client *http.Client) func(context.Context, string, string) (bool, error) {
	return func(ctx context.Context, project, head string) (bool, error) {
		u := api + "/projects/" + url.PathEscape(project) + "/repository/commits/" + head
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return false, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return false, err
		}
		resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusOK:
			return true, nil
		case http.StatusNotFound:
			return false, nil
		}
		return false, errors.New(resp.Status)
	}
}
