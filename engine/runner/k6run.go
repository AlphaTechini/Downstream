package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"downstream/engine/k6gen"
	"downstream/engine/model"
)

// k6Slack is the time k6 gets beyond the pass duration: the scenarios'
// graceful stop plus startup and summary time. A var so tests can shorten it.
var k6Slack = 60 * time.Second

// k6Version returns the first line of `k6 version`.
func k6Version(ctx context.Context, k6 string) (string, error) {
	out, err := exec.CommandContext(ctx, k6, "version").Output()
	if err != nil {
		return "", fmt.Errorf("k6 version: %w", err)
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	if line == "" {
		return "", fmt.Errorf("k6 version printed nothing")
	}
	return strings.TrimSpace(line), nil
}

// runK6 writes the script into a fresh directory under tmp, runs k6 there
// (handleSummary writes steps.json relative to the working directory), and
// returns the parsed steps and the completed-iteration count per journey (nil
// when steps.json is in the older form without them). If k6 is killed or exits non-zero after writing
// steps.json, the figures are still used and the k6 error goes to log; only a
// missing or unreadable steps.json is an error.
func runK6(ctx context.Context, k6, tmp, script string, maxDuration time.Duration, log logger) ([]model.StepMetrics, map[string]int, error) {
	dir, err := os.MkdirTemp(tmp, "k6-")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "script.js"), []byte(script), 0o600); err != nil {
		return nil, nil, err
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, maxDuration+k6Slack)
	defer cancel()
	cmd := exec.CommandContext(ctx, k6, "run", "-q", "--no-color", "--no-usage-report", "--log-output=none", "script.js")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "K6_NO_USAGE_REPORT=true", "NO_PROXY=127.0.0.1", "no_proxy=127.0.0.1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	out := &tailBuffer{max: 4096}
	cmd.Stdout, cmd.Stderr = out, out
	var k6err error
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			k6err = fmt.Errorf("k6 stopped: %w", ctx.Err())
		} else {
			k6err = fmt.Errorf("k6 failed: %w", err)
		}
	}
	if parent.Err() != nil && k6err != nil {
		return nil, nil, fmt.Errorf("%w; output tail:\n%s", k6err, out.String())
	}
	data, err := os.ReadFile(filepath.Join(dir, "steps.json"))
	if err != nil {
		if k6err != nil {
			return nil, nil, fmt.Errorf("%w; no steps.json was written; output tail:\n%s", k6err, out.String())
		}
		return nil, nil, fmt.Errorf("k6 wrote no steps.json: %w", err)
	}
	steps, completed, err := k6gen.ParseSummary(data)
	if err != nil {
		if k6err != nil {
			return nil, nil, fmt.Errorf("%w; %v; output tail:\n%s", k6err, err, out.String())
		}
		return nil, nil, err
	}
	if k6err != nil {
		log.printf("%v; using the steps.json it wrote; output tail: %s", k6err, strings.TrimSpace(out.String()))
	}
	return steps, completed, nil
}
