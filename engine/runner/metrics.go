package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The runtime counters come from a file the engine adds to the target's
// package main at build time with `go build -overlay`. The overlay maps a path
// that does not exist in the extracted tree to a generated file, so neither the
// repository nor the extracted tree is edited, and base and head get the same
// file. The file starts a loopback listener only when the engine sets
// metricsAddrEnv on the app process.

const (
	metricsFileName = "zz_downstream_metrics.go"
	metricsAddrEnv  = "DOWNSTREAM_METRICS_ADDR"
)

// metricsSource is the injected file. Every identifier carries the zzDownstream
// prefix so it cannot clash with the target's own. The route has no method
// prefix because the target's go.mod may select the pre-1.22 ServeMux. A runtime/metrics value that
// is not a uint64 in this Go version is left out, and the engine then treats
// the whole reading as unavailable.
const metricsSource = `package main

import (
	zzDownstreamJSON "encoding/json"
	zzDownstreamHTTP "net/http"
	zzDownstreamOS "os"
	zzDownstreamMetrics "runtime/metrics"
)

func init() {
	zzDownstreamAddr := zzDownstreamOS.Getenv("` + metricsAddrEnv + `")
	if zzDownstreamAddr == "" {
		return
	}
	zzDownstreamNames := []string{
		"/gc/heap/allocs:objects",
		"/gc/heap/allocs:bytes",
		"/gc/cycles/total:gc-cycles",
		"/sched/goroutines:goroutines",
	}
	zzDownstreamKeys := []string{"alloc_objects", "alloc_bytes", "gc_cycles", "goroutines"}
	zzDownstreamMux := zzDownstreamHTTP.NewServeMux()
	zzDownstreamMux.HandleFunc("/metrics", func(w zzDownstreamHTTP.ResponseWriter, r *zzDownstreamHTTP.Request) {
		zzDownstreamSamples := make([]zzDownstreamMetrics.Sample, len(zzDownstreamNames))
		for i, n := range zzDownstreamNames {
			zzDownstreamSamples[i].Name = n
		}
		zzDownstreamMetrics.Read(zzDownstreamSamples)
		zzDownstreamOut := map[string]uint64{}
		for i, s := range zzDownstreamSamples {
			if s.Value.Kind() == zzDownstreamMetrics.KindUint64 {
				zzDownstreamOut[zzDownstreamKeys[i]] = s.Value.Uint64()
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = zzDownstreamJSON.NewEncoder(w).Encode(zzDownstreamOut)
	})
	go func() { _ = zzDownstreamHTTP.ListenAndServe(zzDownstreamAddr, zzDownstreamMux) }()
}
`

// overlayFile is the JSON document go build -overlay reads.
type overlayFile struct {
	Replace map[string]string `json:"Replace"`
}

// writeOverlay writes the injected file and the overlay JSON that maps
// <tree>/<mainDir>/zz_downstream_metrics.go to it, both in dir, and returns
// the path of the overlay JSON. tree must be absolute.
func writeOverlay(dir, tree, mainDir string) (string, error) {
	if !filepath.IsAbs(tree) {
		return "", fmt.Errorf("the extracted tree path %q is not absolute", tree)
	}
	gen := filepath.Join(dir, metricsFileName)
	if err := os.WriteFile(gen, []byte(metricsSource), 0o644); err != nil {
		return "", err
	}
	target := filepath.Join(tree, filepath.FromSlash(mainDir), metricsFileName)
	b, err := json.Marshal(overlayFile{Replace: map[string]string{target: gen}})
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// withOverlayFlag returns env with -overlay=path added to GOFLAGS, keeping
// whatever GOFLAGS the environment already holds. GOFLAGS is split on spaces
// with no quoting, so a path with whitespace cannot be passed.
func withOverlayFlag(env []string, path string) ([]string, error) {
	if strings.ContainsAny(path, " \t\n") {
		return nil, fmt.Errorf("the overlay path %q contains whitespace, which GOFLAGS cannot carry", path)
	}
	flag := "-overlay=" + path
	out := make([]string, 0, len(env)+1)
	found := false
	for _, kv := range env {
		if rest, ok := strings.CutPrefix(kv, "GOFLAGS="); ok {
			found = true
			if strings.TrimSpace(rest) != "" {
				kv = "GOFLAGS=" + rest + " " + flag
			} else {
				kv = "GOFLAGS=" + flag
			}
		}
		out = append(out, kv)
	}
	if !found {
		out = append(out, "GOFLAGS="+flag)
	}
	return out, nil
}

// runtimeCounters is one reading of the injected /metrics endpoint.
type runtimeCounters struct {
	AllocObjects, AllocBytes, GCCycles, Goroutines uint64
}

// parseRuntimeCounters reads the JSON the injected file serves. All four values
// must be present: a missing one means the Go version did not offer it as a
// uint64.
func parseRuntimeCounters(body []byte) (runtimeCounters, error) {
	var raw map[string]*uint64
	if err := json.Unmarshal(body, &raw); err != nil {
		return runtimeCounters{}, fmt.Errorf("the metrics reply is not a JSON object of integers: %w", err)
	}
	var rc runtimeCounters
	for _, f := range []struct {
		key string
		dst *uint64
	}{
		{"alloc_objects", &rc.AllocObjects}, {"alloc_bytes", &rc.AllocBytes},
		{"gc_cycles", &rc.GCCycles}, {"goroutines", &rc.Goroutines},
	} {
		v, ok := raw[f.key]
		if !ok || v == nil {
			return runtimeCounters{}, fmt.Errorf("the metrics reply has no %q value", f.key)
		}
		*f.dst = *v
	}
	return rc, nil
}

// readRuntimeCounters fetches /metrics from the app's side port. The client
// opens a fresh connection each time so no idle connection, or goroutine
// serving one, outlives the read.
func readRuntimeCounters(ctx context.Context, addr string) (runtimeCounters, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/metrics", nil)
	if err != nil {
		return runtimeCounters{}, err
	}
	client := &http.Client{
		Transport: &http.Transport{DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		// The error text carries the port, which differs per pass; the note
		// built from it should read the same in every pass.
		return runtimeCounters{}, errors.New("the injected /metrics endpoint did not answer")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return runtimeCounters{}, fmt.Errorf("the metrics endpoint answered %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return runtimeCounters{}, err
	}
	return parseRuntimeCounters(body)
}

// counterDelta is after minus before, or an error when the counter went
// backwards, which these monotonic counters never do in one process.
func counterDelta(name string, before, after uint64) (uint64, error) {
	if after < before {
		return 0, fmt.Errorf("%s went backwards", name)
	}
	return after - before, nil
}

// firstRuntimeCounters reads the endpoint for the first time in a pass. The
// listener starts in init, but may bind a moment after the app's own port, so
// a refused connection is retried briefly.
func firstRuntimeCounters(ctx context.Context, addr string) (runtimeCounters, error) {
	var rc runtimeCounters
	var err error
	for i := 0; i < 20; i++ {
		if rc, err = readRuntimeCounters(ctx, addr); err == nil {
			return rc, nil
		}
		select {
		case <-ctx.Done():
			return rc, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return rc, err
}
