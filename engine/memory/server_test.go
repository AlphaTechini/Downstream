package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"downstream/engine/history"
)

func testServer(now time.Time) (*Server, *MemStore) {
	st := NewMemStore()
	return &Server{
		Store:     st,
		Allowed:   []string{"g/newgit"},
		Retention: 30 * 24 * time.Hour,
		CommitExists: func(_ context.Context, _, head string) (bool, error) {
			return head != strings.Repeat("f", 40), nil
		},
		Log: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Now: func() time.Time { return now },
	}, st
}

func record(mr int, head string) history.Entry {
	return history.Entry{Version: 1, Project: "g/newgit", MR: mr, Base: strings.Repeat("0", 40), Head: head,
		Metrics: map[string]history.Metric{"app.cpu_ms_per_journey": {Label: "CPU per completed journey", Unit: "ms/journey", Base: 10, C: 11, Change: 0.1}}}
}

func post(t *testing.T, h http.Handler, e history.Entry) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(e)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/runs", bytes.NewReader(b)))
	return w
}

func TestRecordReturnsPriorRecords(t *testing.T) {
	s, _ := testServer(time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC))
	h := s.Handler()
	if w := post(t, h, record(1, strings.Repeat("1", 40))); w.Code != 200 {
		t.Fatalf("first: %d %s", w.Code, w.Body)
	}
	w := post(t, h, record(2, strings.Repeat("2", 40)))
	var resp recordResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || w.Code != 200 {
		t.Fatalf("second: %d %s", w.Code, w.Body)
	}
	if len(resp.Prior) != 1 || resp.Prior[0].MR != 1 || resp.Recorded.Recorded == "" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestRecordRefusesUnknownProjectAndFakeCommit(t *testing.T) {
	s, st := testServer(time.Now())
	h := s.Handler()
	other := record(1, strings.Repeat("1", 40))
	other.Project = "someone/else"
	if w := post(t, h, other); w.Code != http.StatusForbidden {
		t.Fatalf("other project: %d", w.Code)
	}
	if w := post(t, h, record(1, strings.Repeat("f", 40))); w.Code != http.StatusForbidden {
		t.Fatalf("fake commit: %d", w.Code)
	}
	if es, _ := st.List(context.Background(), "g/newgit"); len(es) != 0 {
		t.Fatalf("refused records were stored: %v", es)
	}
}

func TestRecordPrunesOldRecords(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	s, st := testServer(now)
	old := record(1, strings.Repeat("1", 40))
	old.Recorded = now.Add(-31 * 24 * time.Hour).Format(time.RFC3339Nano)
	_ = st.Add(context.Background(), old)
	post(t, s.Handler(), record(2, strings.Repeat("2", 40)))
	es, _ := st.List(context.Background(), "g/newgit")
	if len(es) != 1 || es[0].MR != 2 {
		t.Fatalf("records = %+v, want only the new one", es)
	}
}

func TestDashboardShowsAllowedProjects(t *testing.T) {
	s, _ := testServer(time.Now())
	h := s.Handler()
	post(t, h, record(7, strings.Repeat("7", 40)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, "g/newgit") || !strings.Contains(body, "!7") || !strings.Contains(body, "10.0%") || !strings.Contains(body, `<td class="num">-</td>`) {
		t.Fatalf("dashboard %d:\n%s", w.Code, body)
	}
}
