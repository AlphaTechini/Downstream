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

const k6Slack = 30 * time.Second

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
// returns the parsed steps.
func runK6(ctx context.Context, k6, tmp, script string, maxDuration time.Duration) ([]model.StepMetrics, error) {
	dir, err := os.MkdirTemp(tmp, "k6-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "script.js"), []byte(script), 0o600); err != nil {
		return nil, err
	}
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
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("k6 stopped: %w; output tail:\n%s", ctx.Err(), out.String())
		}
		return nil, fmt.Errorf("k6 failed: %w; output tail:\n%s", err, out.String())
	}
	data, err := os.ReadFile(filepath.Join(dir, "steps.json"))
	if err != nil {
		return nil, fmt.Errorf("k6 wrote no steps.json: %w", err)
	}
	return k6gen.ParseSummary(data)
}
