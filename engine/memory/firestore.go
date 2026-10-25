package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"downstream/engine/history"
)

const metadataURL = "http://metadata.google.internal/computeMetadata/v1/"

// Firestore stores records in the "runs" collection of a Firestore database
// through its REST API, authenticated with the Cloud Run service account's
// token from the metadata server. Each document holds the project, the time it
// was recorded and the record as JSON.
type Firestore struct {
	base   string // https://firestore.googleapis.com/v1/projects/<p>/databases/(default)/documents
	client *http.Client

	mu      sync.Mutex
	token   string
	expires time.Time
}

// NewFirestore finds the GCP project from the metadata server.
func NewFirestore(ctx context.Context) (*Firestore, error) {
	f := &Firestore{client: &http.Client{Timeout: 15 * time.Second}}
	p, err := f.metadata(ctx, "project/project-id")
	if err != nil {
		return nil, fmt.Errorf("reading the project ID from the metadata server: %w", err)
	}
	f.base = "https://firestore.googleapis.com/v1/projects/" + p + "/databases/(default)/documents"
	return f, nil
}

func (f *Firestore) metadata(ctx context.Context, path string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL+path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := f.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("metadata %s: %s", path, resp.Status)
	}
	return strings.TrimSpace(string(b)), nil
}

func (f *Firestore) accessToken(ctx context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.token != "" && time.Until(f.expires) > time.Minute {
		return f.token, nil
	}
	raw, err := f.metadata(ctx, "instance/service-accounts/default/token")
	if err != nil {
		return "", err
	}
	var t struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal([]byte(raw), &t); err != nil || t.AccessToken == "" {
		return "", errors.New("the metadata server returned no access token")
	}
	f.token, f.expires = t.AccessToken, time.Now().Add(time.Duration(t.ExpiresIn)*time.Second)
	return f.token, nil
}

func (f *Firestore) call(ctx context.Context, method, url string, body any, out any) error {
	tok, err := f.accessToken(ctx)
	if err != nil {
		return err
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("firestore %s: %s: %s", method, resp.Status, strings.TrimSpace(string(b[:min(len(b), 300)])))
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

func (f *Firestore) Add(ctx context.Context, e history.Entry) error {
	js, err := json.Marshal(e)
	if err != nil {
		return err
	}
	doc := map[string]any{"fields": map[string]any{
		"project":  map[string]string{"stringValue": e.Project},
		"recorded": map[string]string{"timestampValue": e.Recorded},
		"entry":    map[string]string{"stringValue": string(js)},
	}}
	return f.call(ctx, http.MethodPost, f.base+"/runs", doc, nil)
}

type storedDoc struct {
	name  string
	entry history.Entry
}

// docs returns the project's documents. The single equality filter needs no
// composite index; ordering happens here.
func (f *Firestore) docs(ctx context.Context, project string) ([]storedDoc, error) {
	q := map[string]any{"structuredQuery": map[string]any{
		"from": []map[string]string{{"collectionId": "runs"}},
		"where": map[string]any{"fieldFilter": map[string]any{
			"field": map[string]string{"fieldPath": "project"},
			"op":    "EQUAL",
			"value": map[string]string{"stringValue": project},
		}},
		"limit": 500,
	}}
	var rows []struct {
		Document *struct {
			Name   string `json:"name"`
			Fields struct {
				Entry struct {
					StringValue string `json:"stringValue"`
				} `json:"entry"`
			} `json:"fields"`
		} `json:"document"`
	}
	if err := f.call(ctx, http.MethodPost, f.base+":runQuery", q, &rows); err != nil {
		return nil, err
	}
	var out []storedDoc
	for _, r := range rows {
		if r.Document == nil {
			continue
		}
		var e history.Entry
		if json.Unmarshal([]byte(r.Document.Fields.Entry.StringValue), &e) != nil {
			continue
		}
		out = append(out, storedDoc{name: r.Document.Name, entry: e})
	}
	return out, nil
}

func (f *Firestore) List(ctx context.Context, project string) ([]history.Entry, error) {
	ds, err := f.docs(ctx, project)
	if err != nil {
		return nil, err
	}
	out := make([]history.Entry, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.entry)
	}
	newestFirst(out)
	return out, nil
}

// Prune deletes old records with ordinary deletes: Firestore's TTL deletes are
// not covered by the free tier, these are.
func (f *Firestore) Prune(ctx context.Context, project string, cutoff time.Time) (int, error) {
	ds, err := f.docs(ctx, project)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, d := range ds {
		t, err := time.Parse(time.RFC3339Nano, d.entry.Recorded)
		if err != nil || !t.Before(cutoff) {
			continue
		}
		if err := f.call(ctx, http.MethodDelete, "https://firestore.googleapis.com/v1/"+d.name, nil, nil); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
