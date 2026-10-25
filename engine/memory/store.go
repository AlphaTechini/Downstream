// Package memory is the Downstream memory service: it keeps the numbers-only
// run records of opted-in projects so the flow can compound the changes of
// merged merge requests into a trend.
package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"downstream/engine/history"
)

// Store keeps run records per project.
type Store interface {
	Add(ctx context.Context, e history.Entry) error
	List(ctx context.Context, project string) ([]history.Entry, error)
	// Prune deletes the project's records recorded before cutoff and reports
	// how many it deleted.
	Prune(ctx context.Context, project string, cutoff time.Time) (int, error)
}

// MemStore is an in-memory Store for tests and local runs.
type MemStore struct {
	mu   sync.Mutex
	runs map[string][]history.Entry
}

func NewMemStore() *MemStore { return &MemStore{runs: map[string][]history.Entry{}} }

func (m *MemStore) Add(_ context.Context, e history.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[e.Project] = append(m.runs[e.Project], e)
	return nil
}

func (m *MemStore) List(_ context.Context, project string) ([]history.Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]history.Entry(nil), m.runs[project]...)
	newestFirst(out)
	return out, nil
}

func (m *MemStore) Prune(_ context.Context, project string, cutoff time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	keep := m.runs[project][:0]
	n := 0
	for _, e := range m.runs[project] {
		if t, err := time.Parse(time.RFC3339Nano, e.Recorded); err == nil && t.Before(cutoff) {
			n++
			continue
		}
		keep = append(keep, e)
	}
	m.runs[project] = keep
	return n, nil
}

func newestFirst(es []history.Entry) {
	sort.SliceStable(es, func(i, k int) bool { return es[i].Recorded > es[k].Recorded })
}
