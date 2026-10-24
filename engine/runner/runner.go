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
	"slices"
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

// passBudget is the time the three measured passes may take together, so a
// run fits the flow's 200 s command limit alongside the two builds. Each pass
// gets an equal share, capped by the target's own duration.
const passBudget = 120 * time.Second

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
	if !cfg.HasBackend() {
		return fail("load config", errors.New("downstream.yml declares only a frontend; there is no backend to measure, so use the frontend command"))
	}
	var compare, fresh []model.Journey
	for _, j := range o.Journeys.Journeys {
		if j.IsNew() {
			fresh = append(fresh, j)
		} else {
			compare = append(compare, j)
		}
	}
	if share := passBudget / 3; cfg.Duration > share {
		cfg.Duration = share
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

	builds := map[string]buildOut{}
	for _, side := range []struct{ name, sha string }{{"base", baseSHA}, {"head", headSHA}} {
		if side.name == "head" && headSHA == baseSHA {
			builds["head"] = builds["base"]
			log.printf("head is the same commit as base; building once")
			break
		}
		b := buildOut{tree: filepath.Join(tmp, side.name+"-src"), bin: filepath.Join(tmp, "bin", side.name+"-app")}
		stage := "build (" + side.name + ")"
		log.printf("%s: extracting %s", stage, short(side.sha))
		if err := os.Mkdir(b.tree, 0o755); err != nil {
			return fail(stage, err)
		}
		if err := materialise(ctx, o.Repo, side.sha, b.tree); err != nil {
			return fail(stage, fmt.Errorf("extracting the commit: %w", err))
		}
		log.printf("%s: %s", stage, cfg.BuildCommand(b.bin))
		metrics, note, err := buildTarget(ctx, cfg, tmp, side.name, b.tree, b.bin)
		if err != nil {
			return fail(stage, err)
		}
		b.metrics = metrics
		if note != "" && !slices.Contains(run.NotMeasured, note) {
			run.NotMeasured = append(run.NotMeasured, note)
			log.printf("%s: %s", stage, note)
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
	if len(compare) == 0 {
		return fail("baseline check", errors.New("every journey uses new routes, so nothing can be compared; add existing or control journeys"))
	}
	probe, _, err := runPass(ctx, passSpec{k6: k6, tmp: tmp, log: log, cfg: cfg, b: builds["base"], runID: runID, vus: 1, iterations: 1},
		model.JourneyFile{Version: o.Journeys.Version, Journeys: compare})
	if err != nil {
		return fail("baseline check", err)
	}
	var kept []model.Journey
	for _, j := range compare {
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

	// The head pass carries the new journeys too, as real users would use the new
	// feature alongside the old ones, so existing pages are measured while it is
	// in use. Their steps are moved out of C afterwards, so the old-against-new
	// tables compare the same journeys.
	mixedFile := keptFile
	if len(fresh) > 0 {
		mixedFile = model.JourneyFile{Version: o.Journeys.Version, Journeys: append(append([]model.Journey{}, kept...), fresh...)}
	}
	for _, p := range []struct{ label, side, sha string }{
		{"B1", "base", baseSHA}, {"C", "head", headSHA}, {"B2", "base", baseSHA},
	} {
		file := keptFile
		if p.label == "C" {
			file = mixedFile
		}
		log.printf("pass %s: %s %s, %d journeys", p.label, p.side, short(p.sha), len(file.Journeys))
		res, notes, err := runPass(ctx, passSpec{k6: k6, tmp: tmp, log: log, cfg: cfg, b: builds[p.side], runID: runID, vus: cfg.VUs, iterations: cfg.Iterations, measure: true}, file)
		if err != nil {
			return fail("pass "+p.label, err)
		}
		for _, n := range notes {
			if !slices.Contains(run.NotMeasured, n) {
				run.NotMeasured = append(run.NotMeasured, n)
			}
		}
		res.Label, res.Commit = p.label, p.sha
		if p.label == "C" && len(fresh) > 0 {
			nf := &model.NewFeaturePass{Journeys: fresh, Pass: res}
			nf.Pass.Steps, res.Steps = splitSteps(res.Steps, fresh)
			run.NewFeature = nf
		}
		run.Passes = append(run.Passes, res)
	}
	log.printf("done: %d passes", len(run.Passes))
	return nil
}

// splitSteps separates the metrics of the given journeys from the rest.
func splitSteps(steps []model.StepMetrics, js []model.Journey) (of, rest []model.StepMetrics) {
	ids := map[string]bool{}
	for _, j := range js {
		ids[j.ID] = true
	}
	for _, s := range steps {
		if ids[s.Journey] {
			of = append(of, s)
		} else {
			rest = append(rest, s)
		}
	}
	return of, rest
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

// buildOut is one extracted and built commit.
type buildOut struct {
	tree, bin string
	// metrics is true when the binary carries the injected counters file.
	metrics bool
}

// buildTarget builds tree, with the runtime-counter file injected when the
// config names a main_dir and the build accepts it. It never fails because of
// the injection: when the file cannot be added or the build refuses it, the
// target is built as is and the second result says why the counters are not
// measured. The error is the plain build's.
func buildTarget(ctx context.Context, cfg target.Config, tmp, side, tree, bin string) (bool, string, error) {
	plain := func(reason string) (bool, string, error) {
		return false, "runtime counters: not measured; " + reason, buildTree(ctx, cfg, tree, bin, os.Environ())
	}
	if cfg.MainDir == "" {
		return plain("downstream.yml sets no main_dir, so no counter code is added to the build.")
	}
	if st, err := os.Stat(filepath.Join(tree, filepath.FromSlash(cfg.MainDir))); err != nil || !st.IsDir() {
		return plain("main_dir " + cfg.MainDir + " is not a directory in the " + side + " commit.")
	}
	dir := filepath.Join(tmp, "overlay-"+side)
	if err := os.Mkdir(dir, 0o755); err != nil {
		return plain("the overlay directory could not be created (" + errText(err) + ").")
	}
	overlay, err := writeOverlay(dir, tree, cfg.MainDir)
	if err != nil {
		return plain("the overlay could not be written (" + errText(err) + ").")
	}
	env, err := withOverlayFlag(os.Environ(), overlay)
	if err != nil {
		return plain(err.Error() + ".")
	}
	err = buildTree(ctx, cfg, tree, bin, env)
	if err == nil {
		return true, "", nil
	}
	if ctx.Err() != nil {
		return false, "", err
	}
	_ = os.Remove(bin)
	return plain("the " + side + " build with the injected file failed, so it was built without it (" +
		lastLine(strings.ReplaceAll(err.Error(), tree, "<tree>")) + ").")
}

// lastLine returns the last non-empty line of s, trimmed, which is where go
// build prints its error.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if len(last) > 200 {
		last = last[:200]
	}
	return last
}

// buildTree runs the configured build command in tree with env.
func buildTree(ctx context.Context, cfg target.Config, tree, bin string, env []string) error {
	ctx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", cfg.BuildCommand(bin))
	cmd.Dir = tree
	cmd.Env = env
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
	log        logger
	cfg        target.Config
	b          buildOut
	runID      string
	vus        int
	iterations int
	// measure turns on the process counters beyond CPU ticks and peak RSS. The
	// one-iteration baseline check leaves it off: its figures are not reported.
	measure bool
}

// connDrain is how long runPass waits after k6 exits before it counts open
// file descriptors and threads. k6 closes its keep-alive connections when it
// exits, but the app only drops its side when it reads the EOF, so a count taken
// at once depends on how fast the app noticed. A var so tests can shorten it.
var connDrain = 500 * time.Millisecond

// runPass starts a fresh app, runs k6 against it once, samples /proc around
// the k6 run, and stops the app on every path. The second result holds the
// Run.NotMeasured lines for counters that could not be read.
func runPass(ctx context.Context, p passSpec, jf model.JourneyFile) (model.Pass, []string, error) {
	var res model.Pass
	port, err := pickPort()
	if err != nil {
		return res, nil, fmt.Errorf("choosing a port: %w", err)
	}
	// The side port is only for passes that report the runtime counters.
	var env []string
	var rtAddr string
	if p.measure && p.b.metrics {
		mport := port
		for mport == port {
			if mport, err = pickPort(); err != nil {
				return res, nil, fmt.Errorf("choosing a port: %w", err)
			}
		}
		rtAddr = "127.0.0.1:" + strconv.Itoa(mport)
		env = append(env, metricsAddrEnv+"="+rtAddr)
	}
	a, err := startApp(p.cfg, p.b.bin, p.b.tree, port, env...)
	if err != nil {
		return res, nil, fmt.Errorf("starting the app: %w", err)
	}
	defer a.stop()
	if err := a.waitReady(ctx, p.cfg.ReadyPath); err != nil {
		return res, nil, err
	}
	script, err := k6gen.Script(jf, k6gen.Options{
		BaseURL: a.baseURL(), VUs: p.vus, Iterations: p.iterations, MaxDuration: p.cfg.Duration, RunID: p.runID,
	})
	if err != nil {
		return res, nil, err
	}

	dir, err := procDir(a.pid)
	if err != nil {
		return res, nil, fmt.Errorf("finding the app in /proc: %w", err)
	}
	var s procSample
	// A failure to read the side port never fails the pass: the counters are
	// then recorded as not measured. The reads go to the side port, so they are
	// not journey traffic.
	switch {
	case !p.measure:
	case rtAddr == "":
		s.rtErr, s.rtQuiet = errors.New("the build has no injected counters file"), true
	default:
		if s.rt0, s.rtErr = firstRuntimeCounters(ctx, rtAddr); s.rtErr != nil && ctx.Err() != nil {
			return res, nil, ctx.Err()
		}
	}
	if s.ticks0, err = readCPUTicks(dir); err != nil {
		return res, nil, fmt.Errorf("reading app CPU time: %w", err)
	}
	if p.measure {
		s.io0, s.ioErr = procSource.io(dir)
	}
	if p.measure {
		s.sched0, s.schedErr = readSchedstat(dir)
	}
	started := time.Now()
	steps, completed, received, kerr := runK6(ctx, p.k6, p.tmp, script, p.cfg.Duration, p.log)
	ended := time.Now()
	if a.exited() {
		return res, nil, fmt.Errorf("%w; output tail:\n%s", errAppDied, a.out.String())
	}
	if kerr != nil {
		return res, nil, kerr
	}
	if s.ticks1, err = readCPUTicks(dir); err != nil {
		return res, nil, fmt.Errorf("reading app CPU time: %w", err)
	}
	if p.measure {
		if s.schedErr == nil {
			s.sched1, s.schedErr = readSchedstat(dir)
			s.schedOK = s.schedErr == nil
		}
		io1, ioErr := procSource.io(dir)
		s.io1 = io1
		if s.ioErr == nil {
			s.ioErr = ioErr
		}
	}
	if p.measure && s.rtErr == nil {
		s.rt1, s.rtErr = readRuntimeCounters(ctx, rtAddr)
	}
	if s.rssMB, err = readPeakRSSMB(dir); err != nil {
		return res, nil, fmt.Errorf("reading app peak memory: %w", err)
	}
	s.received = received
	for _, n := range completed {
		s.completed += n
	}
	res.StartedAt, res.EndedAt = stamp(started), stamp(ended)
	res.Steps = steps

	if !p.measure {
		res.Process = model.ProcessStats{CPUSeconds: float64(s.ticks1-s.ticks0) / clockTicksPerSecond, PeakRSSMB: s.rssMB}
		return res, nil, nil
	}

	select {
	case <-time.After(connDrain):
	case <-a.done:
		return res, nil, fmt.Errorf("%w; output tail:\n%s", errAppDied, a.out.String())
	case <-ctx.Done():
		return res, nil, ctx.Err()
	}
	s.threads, s.threadsErr = procSource.threads(dir)
	s.fds, s.fdsErr = procSource.fds(dir)
	if s.rtErr == nil {
		s.rtDrained, s.rtErr = readRuntimeCounters(ctx, rtAddr)
	}

	// Stop now rather than at return so the kernel's resource usage for the
	// exited process is available. The deferred stop is then a no-op.
	a.stop()
	if a.exited() {
		s.exactCPU, s.haveExact = rusageSeconds(a.cmd.ProcessState)
	}
	var notes []string
	res.Process, notes = buildProcess(s)
	return res, notes, nil
}
