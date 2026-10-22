// Package runner builds the base and head commits, starts each build on a
// loopback port, drives it with k6 and records what k6 and /proc report. It
// does no classification; that is engine/classify's job.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"downstream/engine/journey"
	"downstream/engine/k6gen"
	"downstream/engine/model"
	"downstream/engine/scan"
	"downstream/engine/target"
)

const buildTimeout = 150 * time.Second

// Options configure one run.
type Options struct {
	Repo, Base, Head string
	Journeys         model.JourneyFile
	K6               string    // path to k6; the PATH is searched when empty
	Log              io.Writer // one progress line per stage; nil discards
}

const baselineReason = "failed on the baseline (expected status not returned or a value could not be extracted)"

// stageError says which stage stopped the run.
type stageError struct {
	stage string
	err   error
}

func (e *stageError) Error() string { return e.stage + ": " + e.err.Error() }
func (e *stageError) Unwrap() error { return e.err }

func fail(stage string, err error) error { return &stageError{stage: stage, err: err} }

// Run always returns a populated model.Run. When it stops early, Run.Error is
// set and the returned error is non-nil.
func Run(ctx context.Context, o Options) (model.Run, error) {
	run := model.Run{
		Version:     model.FormatVersion,
		StartedAt:   stamp(time.Now()),
		Journeys:    []model.Journey{},
		Discarded:   []model.Discard{},
		Passes:      []model.Pass{},
		Findings:    []model.Finding{},
		NotMeasured: []string{"DB time: not measured; the target exposes no database timing."},
	}
	err := execute(ctx, o, &run)
	run.EndedAt = stamp(time.Now())
	if err != nil {
		var se *stageError
		if errors.As(err, &se) {
			run.Error = &model.RunError{Stage: se.stage, Message: se.err.Error()}
		} else {
			run.Error = &model.RunError{Stage: "run", Message: err.Error()}
		}
	}
	return run, err
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

type logger struct{ w io.Writer }

func (l logger) printf(format string, a ...any) {
	if l.w != nil {
		fmt.Fprintf(l.w, "downstream: "+format+"\n", a...)
	}
}

func execute(ctx context.Context, o Options, run *model.Run) error {
	log := logger{o.Log}

	k6 := o.K6
	if k6 == "" {
		p, err := exec.LookPath("k6")
		if err != nil {
			return fail("k6", errors.New("k6 was not found on PATH"))
		}
		k6 = p
	}
	ver, err := k6Version(ctx, k6)
	if err != nil {
		return fail("k6", err)
	}
	run.Settings.K6Version = ver

	baseSHA, err := resolveCommit(ctx, o.Repo, o.Base)
	if err != nil {
		return fail("resolve commits", err)
	}
	headSHA, err := resolveCommit(ctx, o.Repo, o.Head)
	if err != nil {
		return fail("resolve commits", err)
	}
	run.Base, run.Head = baseSHA, headSHA
	log.printf("base %s, head %s, %s", short(baseSHA), short(headSHA), ver)

	cfg, err := target.Load(o.Repo, baseSHA)
	if err != nil {
		return fail("load config", err)
	}
	run.Settings.VUs = cfg.VUs
	run.Settings.Iterations = cfg.Iterations
	run.Settings.DurationSeconds = int(cfg.Duration / time.Second)

	tmp, err := os.MkdirTemp("", "downstream-run-")
	if err != nil {
		return fail("temp dir", err)
	}
	defer os.RemoveAll(tmp)
	if err := os.Mkdir(filepath.Join(tmp, "bin"), 0o755); err != nil {
		return fail("temp dir", err)
	}

	type build struct{ tree, bin string }
	builds := map[string]build{}
	for _, side := range []struct{ name, sha string }{{"base", baseSHA}, {"head", headSHA}} {
		if side.name == "head" && headSHA == baseSHA {
			builds["head"] = builds["base"]
			log.printf("head is the same commit as base; building once")
			break
		}
		b := build{tree: filepath.Join(tmp, side.name+"-src"), bin: filepath.Join(tmp, "bin", side.name+"-app")}
		stage := "build (" + side.name + ")"
		log.printf("%s: extracting %s", stage, short(side.sha))
		if err := os.Mkdir(b.tree, 0o755); err != nil {
			return fail(stage, err)
		}
		if err := materialise(ctx, o.Repo, side.sha, b.tree); err != nil {
			return fail(stage, fmt.Errorf("extracting the commit: %w", err))
		}
		log.printf("%s: %s", stage, cfg.BuildCommand(b.bin))
		if err := buildTree(ctx, cfg, b.tree, b.bin); err != nil {
			return fail(stage, err)
		}
		builds[side.name] = b
	}

	log.printf("validate journeys")
	routes, err := scan.Routes(filepath.Join(builds["head"].tree, filepath.FromSlash(cfg.Root)))
	if err != nil {
		return fail("validate journeys", fmt.Errorf("reading the head route table: %w", err))
	}
	if err := journey.Validate(o.Journeys, routes); err != nil {
		return fail("validate journeys", err)
	}

	runID := strconv.FormatInt(time.Now().Unix(), 16)

	log.printf("baseline check: one iteration of each journey on base")
	probe, err := runPass(ctx, passSpec{k6: k6, tmp: tmp, cfg: cfg, b: builds["base"], runID: runID, vus: 1, iterations: 1}, o.Journeys)
	if err != nil {
		return fail("baseline check", err)
	}
	var kept []model.Journey
	for _, j := range o.Journeys.Journeys {
		if d, bad := baselineFailure(j, probe.Steps); bad {
			run.Discarded = append(run.Discarded, d)
			log.printf("baseline check: discarded %s at step %s", d.Journey, d.Step)
			continue
		}
		kept = append(kept, j)
	}
	if len(kept) == 0 {
		return fail("baseline check", errors.New("No journey passed on the baseline, so there is nothing to compare."))
	}
	run.Journeys = kept
	keptFile := model.JourneyFile{Version: o.Journeys.Version, Journeys: kept}

	for _, p := range []struct{ label, side, sha string }{
		{"B1", "base", baseSHA}, {"C", "head", headSHA}, {"B2", "base", baseSHA},
	} {
		log.printf("pass %s: %s %s", p.label, p.side, short(p.sha))
		res, err := runPass(ctx, passSpec{k6: k6, tmp: tmp, cfg: cfg, b: builds[p.side], runID: runID, vus: cfg.VUs, iterations: cfg.Iterations}, keptFile)
		if err != nil {
			return fail("pass "+p.label, err)
		}
		res.Label, res.Commit = p.label, p.sha
		run.Passes = append(run.Passes, res)
	}
	log.printf("done: %d passes", len(run.Passes))
	return nil
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// resolveCommit turns a ref or abbreviated SHA into a full commit SHA.
func resolveCommit(ctx context.Context, repo, ref string) (string, error) {
	if ref == "" || strings.HasPrefix(ref, "-") {
		return "", fmt.Errorf("invalid commit %q", ref)
	}
	out, err := exec.CommandContext(ctx, "git", "-C", repo, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}").Output()
	if err != nil {
		return "", fmt.Errorf("cannot resolve %q to a commit: %w", ref, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// buildTree runs the configured build command in tree.
func buildTree(ctx context.Context, cfg target.Config, tree, bin string) error {
	ctx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", cfg.BuildCommand(bin))
	cmd.Dir = tree
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	out := &tailBuffer{max: 4096}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("build stopped (%w); output tail:\n%s", ctx.Err(), out.String())
		}
		return fmt.Errorf("build failed (%w); output tail:\n%s", err, out.String())
	}
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("the build command succeeded but wrote no binary at the {out} path: %w", err)
	}
	return nil
}

// baselineFailure reports the first step of j that did not pass on the baseline.
func baselineFailure(j model.Journey, steps []model.StepMetrics) (model.Discard, bool) {
	by := map[string]model.StepMetrics{}
	for _, s := range steps {
		if s.Journey == j.ID {
			by[s.Step] = s
		}
	}
	for _, s := range j.Steps {
		m, ok := by[s.Name]
		switch {
		case !ok || m.Count == 0:
			return model.Discard{Journey: j.ID, Step: s.Name, Reason: "did not run on the baseline (no request was recorded for this step)"}, true
		case m.Failures > 0:
			return model.Discard{Journey: j.ID, Step: s.Name, Reason: baselineReason}, true
		}
	}
	return model.Discard{}, false
}

type passSpec struct {
	k6, tmp    string
	cfg        target.Config
	b          struct{ tree, bin string }
	runID      string
	vus        int
	iterations int
}

// runPass starts a fresh app, runs k6 against it once, samples /proc around
// the k6 run, and stops the app on every path.
func runPass(ctx context.Context, p passSpec, jf model.JourneyFile) (model.Pass, error) {
	var res model.Pass
	port, err := pickPort()
	if err != nil {
		return res, fmt.Errorf("choosing a port: %w", err)
	}
	a, err := startApp(p.cfg, p.b.bin, p.b.tree, port)
	if err != nil {
		return res, fmt.Errorf("starting the app: %w", err)
	}
	defer a.stop()
	if err := a.waitReady(ctx, p.cfg.ReadyPath); err != nil {
		return res, err
	}
	script, err := k6gen.Script(jf, k6gen.Options{
		BaseURL: a.baseURL(), VUs: p.vus, Iterations: p.iterations, MaxDuration: p.cfg.Duration, RunID: p.runID,
	})
	if err != nil {
		return res, err
	}

	ticks0, err := readCPUTicks(a.pid)
	if err != nil {
		return res, fmt.Errorf("reading app CPU time: %w", err)
	}
	started := time.Now()
	steps, kerr := runK6(ctx, p.k6, p.tmp, script, p.cfg.Duration)
	ended := time.Now()
	if a.exited() {
		return res, fmt.Errorf("%w; output tail:\n%s", errAppDied, a.out.String())
	}
	if kerr != nil {
		return res, kerr
	}
	ticks1, err := readCPUTicks(a.pid)
	if err != nil {
		return res, fmt.Errorf("reading app CPU time: %w", err)
	}
	rss, err := readPeakRSSMB(a.pid)
	if err != nil {
		return res, fmt.Errorf("reading app peak memory: %w", err)
	}
	res.StartedAt, res.EndedAt = stamp(started), stamp(ended)
	res.Process = model.ProcessStats{CPUSeconds: float64(ticks1-ticks0) / clockTicksPerSecond, PeakRSSMB: rss}
	res.Steps = steps
	return res, nil
}
