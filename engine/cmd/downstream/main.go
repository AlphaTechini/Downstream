// Command downstream is the engine the Downstream flow calls. Every subcommand
// takes fixed flags; nothing here runs text that a model wrote.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"syscall"
	"unicode"

	"downstream/engine/classify"
	"downstream/engine/history"
	"downstream/engine/issues"
	"downstream/engine/journey"
	"downstream/engine/model"
	"downstream/engine/report"
	"downstream/engine/runner"
	"downstream/engine/scan"
	"downstream/engine/target"
)

const usage = `usage:
  downstream version
  downstream scan     --base <sha> --head <sha> [--repo .] --out scan.json
  downstream schema   journeys
  downstream validate --journeys journeys.json --scan scan.json
  downstream run      --base <sha> --head <sha> --journeys journeys.json [--repo .] [--k6 path] --out run.json
  downstream report   [--run run.json] [--scan scan.json] [--frontend frontend.json] [--trend trend.json] --out report.md
  downstream summary  --run run.json --scan scan.json
  downstream files    --scan scan.json
  downstream show     --scan scan.json --head <sha> --file N [--repo .]
  downstream issues   [--run run.json] [--scan scan.json] [--notes notes.json] [--frontend frontend.json] --mr <iid> --starter <username> --out issues.json
  downstream frontend --base <sha> --head <sha> --journeys fe-journeys.json --fecheck <dir> [--repo .] --out frontend.json
  downstream frontend --detect --base <sha> --head <sha> [--repo .]
  downstream history  record --run run.json --mr <iid> [--project path] [--repo .] --out history.json
  downstream history  trend --record history.json --response memory.json [--service url] [--repo .] --out trend.json
  downstream marker   parse [--journeys-out journeys.json] < issue_body.md
  downstream fixcheck --marker marker.json --option N
`

var shaPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmds := map[string]func([]string) error{
		"version":  cmdVersion,
		"scan":     cmdScan,
		"schema":   cmdSchema,
		"validate": cmdValidate,
		"run":      cmdRun,
		"report":   cmdReport,
		"summary":  cmdSummary,
		"files":    cmdFiles,
		"show":     cmdShow,
		"issues":   cmdIssues,
		"marker":   cmdMarker,
		"fixcheck": cmdFixcheck,
		"frontend": cmdFrontend,
		"history":  cmdHistory,
	}
	cmd, ok := cmds[os.Args[1]]
	if !ok {
		fmt.Fprintf(os.Stderr, "downstream: unknown command %q\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err := cmd(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "downstream "+os.Args[1]+": "+err.Error())
		os.Exit(1)
	}
}

func cmdVersion(args []string) error {
	if len(args) != 0 {
		return errors.New("takes no arguments")
	}
	fmt.Printf("downstream engine, file format v%d, %s\n", model.FormatVersion, runtime.Version())
	return nil
}

func cmdScan(args []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	base := fs.String("base", "", "baseline commit SHA")
	head := fs.String("head", "", "candidate commit SHA")
	repo := fs.String("repo", ".", "repository root")
	out := fs.String("out", "", "output file")
	if err := parse(fs, args, "base", "head", "out"); err != nil {
		return err
	}
	if err := commits(*repo, *base, *head); err != nil {
		return err
	}
	cfg, err := target.Load(*repo, *base)
	if err != nil {
		return err
	}
	if !cfg.HasBackend() {
		return errors.New("downstream.yml declares only a frontend, so there are no backend routes to scan; use the frontend command")
	}
	s, err := scan.Run(scan.Options{Repo: *repo, Base: *base, Head: *head, Root: cfg.Root})
	if err != nil {
		return err
	}
	return writeJSON(*out, s)
}

func cmdSchema(args []string) error {
	if len(args) != 1 || args[0] != "journeys" {
		return errors.New("usage: downstream schema journeys")
	}
	fmt.Println(journey.Schema())
	return nil
}

func cmdValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	jpath := fs.String("journeys", "", "journeys.json")
	spath := fs.String("scan", "", "scan.json")
	if err := parse(fs, args, "journeys", "scan"); err != nil {
		return err
	}
	return validate(os.Stdout, *jpath, *spath)
}

// validate fails only on invalid journeys. Lint findings are printed as
// warnings after the ok line and never change the result.
func validate(w io.Writer, jpath, spath string) error {
	f, err := readJourneys(jpath)
	if err != nil {
		return err
	}
	var s model.Scan
	if err := readJSON(spath, &s); err != nil {
		return err
	}
	if err := journey.Validate(f, s.Routes); err != nil {
		return err
	}
	uncovered, err := journey.Coverage(f, s)
	if err != nil {
		return err
	}
	steps, kinds := 0, map[string]int{}
	for _, j := range f.Journeys {
		steps += len(j.Steps)
		k := j.Kind
		if k == "" {
			k = model.KindExisting
		}
		kinds[k]++
	}
	fmt.Fprintf(w, "ok: %d journeys (%d existing, %d new, %d control), %d steps\n",
		len(f.Journeys), kinds[model.KindExisting], kinds[model.KindNew], kinds[model.KindControl], steps)
	for _, r := range uncovered {
		fmt.Fprintf(w, "uncovered: %s %s (journey cap reached)\n", r.Method, r.Path)
	}
	for _, msg := range journey.Lint(f, s) {
		fmt.Fprintf(w, "warning: %s\n", msg)
	}
	return nil
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	base := fs.String("base", "", "baseline commit SHA")
	head := fs.String("head", "", "candidate commit SHA")
	jpath := fs.String("journeys", "", "journeys.json")
	repo := fs.String("repo", ".", "repository root")
	k6 := fs.String("k6", "", "path to k6 (default: search PATH)")
	out := fs.String("out", "", "output file")
	if err := parse(fs, args, "base", "head", "journeys", "out"); err != nil {
		return err
	}
	if err := commits(*repo, *base, *head); err != nil {
		return err
	}
	f, err := readJourneys(*jpath)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	run, runErr := runner.Run(ctx, runner.Options{
		Repo: *repo, Base: *base, Head: *head, Journeys: f, K6: *k6, Log: os.Stderr,
	})
	run.Settings.Thresholds = classify.DefaultThresholds()
	run.Findings = classify.Classify(run.Passes, run.Settings.Thresholds)
	if run.Findings == nil {
		run.Findings = []model.Finding{}
	}
	// run.json is written even when the run stopped, so the report can say where.
	if err := writeJSON(*out, run); err != nil {
		return err
	}
	return runErr
}

func cmdReport(args []string) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	rpath := fs.String("run", "", "run.json")
	spath := fs.String("scan", "", "scan.json")
	fpath := fs.String("frontend", "", "frontend.json; adds the Frontend section")
	tpath := fs.String("trend", "", "trend.json; adds the Trend section")
	out := fs.String("out", "", "output file")
	if err := parse(fs, args, "out"); err != nil {
		return err
	}
	if *rpath == "" && *fpath == "" {
		return errors.New("need --run, --frontend or both")
	}
	var text string
	if *rpath != "" {
		var run model.Run
		if err := readJSON(*rpath, &run); err != nil {
			return err
		}
		s, err := optionalScan(*spath)
		if err != nil {
			return err
		}
		text = report.Render(run, s)
	}
	if *fpath != "" {
		var f model.Frontend
		if err := readJSON(*fpath, &f); err != nil {
			return err
		}
		if text != "" {
			text += "\n"
		} else {
			text = "### Downstream report\n\n"
		}
		text += report.RenderFrontend(f)
	}
	if *tpath != "" && *rpath != "" {
		var t history.Trend
		if err := readJSON(*tpath, &t); err != nil {
			return err
		}
		text += "\n" + report.RenderTrend(t)
	}
	return os.WriteFile(*out, []byte(text), 0o644)
}

// cmdSummary prints one count per line so the flow and CI can branch on it
// without reading the report. regression counts the issues that would open;
// advisory counts latency regressions, which open none.
func cmdSummary(args []string) error {
	fs := flag.NewFlagSet("summary", flag.ContinueOnError)
	rpath := fs.String("run", "", "run.json")
	spath := fs.String("scan", "", "scan.json; issues are grouped by the changed functions on each route")
	if err := parse(fs, args, "run", "scan"); err != nil {
		return err
	}
	var run model.Run
	if err := readJSON(*rpath, &run); err != nil {
		return err
	}
	scan, err := optionalScan(*spath)
	if err != nil {
		return err
	}
	writeSummary(os.Stdout, run, scan)
	return nil
}

// writeSummary prints the counts. Only findings that may decide count in all
// but the advisory line: latency is advisory and has its own line.
func writeSummary(w io.Writer, run model.Run, scan *model.Scan) {
	counts := map[string]int{}
	tradeOffs := map[string]bool{}
	for _, f := range run.Findings {
		if !f.Decides() {
			continue
		}
		counts[f.Class]++
		if f.Class == model.ClassTradeOff {
			tradeOffs[f.Journey] = true
		}
	}
	fmt.Fprintf(w, "regression=%d\n", len(issues.Headlines(run, scan)))
	fmt.Fprintf(w, "trade-off=%d\n", len(tradeOffs))
	fmt.Fprintf(w, "improved=%d\n", counts[model.ClassImproved])
	fmt.Fprintf(w, "withheld=%d\n", counts[model.ClassWithheld])
	fmt.Fprintf(w, "advisory=%d\n", len(issues.AdvisoryRegressions(run)))
	fmt.Fprintf(w, "discarded=%d\n", len(run.Discarded))
	fmt.Fprintf(w, "passes=%d\n", len(run.Passes))
	fmt.Fprintf(w, "new_failures=%d\n", report.NewFailures(run))
	if run.Error != nil {
		fmt.Fprintf(w, "stopped=%s\n", run.Error.Stage)
	}
}

// readableFiles is the list of files the model may read, in the order the
// numbers refer to: the files the diff changed and the file of every route in
// the blast radius, de-duplicated and sorted. A path that is not a plain
// relative path inside the repository, or that holds a control character, is
// left out, so a line of the list is always one path.
func readableFiles(s *model.Scan) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p == "" || seen[p] || !filepath.IsLocal(p) || strings.ContainsFunc(p, unicode.IsControl) {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, f := range s.ChangedFiles {
		add(f.Path)
	}
	for _, im := range s.Changed {
		add(im.File)
		for _, f := range im.HandlerFiles {
			add(f)
		}
	}
	for _, im := range s.Affected {
		add(im.File)
		for _, f := range im.HandlerFiles {
			add(f)
		}
	}
	sort.Strings(out)
	return out
}

func cmdFiles(args []string) error {
	fs := flag.NewFlagSet("files", flag.ContinueOnError)
	spath := fs.String("scan", "", "scan.json")
	if err := parse(fs, args, "scan"); err != nil {
		return err
	}
	var s model.Scan
	if err := readJSON(*spath, &s); err != nil {
		return err
	}
	listFiles(os.Stdout, readableFiles(&s))
	return nil
}

// listFiles prints one "N<tab>path" line per file, numbered from 1.
func listFiles(w io.Writer, files []string) {
	for i, p := range files {
		fmt.Fprintf(w, "%d\t%s\n", i+1, p)
	}
}

func cmdShow(args []string) error {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	spath := fs.String("scan", "", "scan.json")
	head := fs.String("head", "", "candidate commit SHA")
	n := fs.Int("file", 0, "file number from `downstream files`")
	repo := fs.String("repo", ".", "repository root")
	if err := parse(fs, args, "scan", "head", "file"); err != nil {
		return err
	}
	if err := commits(*repo, *head); err != nil {
		return err
	}
	var s model.Scan
	if err := readJSON(*spath, &s); err != nil {
		return err
	}
	return showFile(os.Stdout, *repo, *head, readableFiles(&s), *n)
}

// showFile writes the content of entry n (1-based) of files at commit head.
// The path goes to git as one argument; no shell is involved.
func showFile(w io.Writer, repo, head string, files []string, n int) error {
	if n < 1 || n > len(files) {
		return fmt.Errorf("--file %d is out of range: the list has %d files", n, len(files))
	}
	var stderr strings.Builder
	cmd := exec.Command("git", "-C", repo, "show", head+":"+files[n-1])
	cmd.Stdout, cmd.Stderr = w, &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git show %s: %w: %s", files[n-1], err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

type issuesOut struct {
	Issues     []issues.Issue `json:"issues"`
	Questions  []string       `json:"questions"`
	NeedsNotes []string       `json:"needs_notes"`
}

func cmdIssues(args []string) error {
	fs := flag.NewFlagSet("issues", flag.ContinueOnError)
	rpath := fs.String("run", "", "run.json")
	spath := fs.String("scan", "", "scan.json")
	npath := fs.String("notes", "", "notes.json written by the model")
	fpath := fs.String("frontend", "", "frontend.json; adds one issue per frontend root cause")
	mr := fs.Int("mr", 0, "merge request IID")
	starter := fs.String("starter", "", "username of whoever started the run")
	out := fs.String("out", "", "output file")
	if err := parse(fs, args, "mr", "starter", "out"); err != nil {
		return err
	}
	if *rpath == "" && *fpath == "" {
		return errors.New("need --run, --frontend or both")
	}
	var notes *model.Notes
	if *npath != "" {
		data, err := os.ReadFile(*npath)
		if err != nil {
			return err
		}
		var n model.Notes
		dec := json.NewDecoder(strings.NewReader(string(data)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&n); err != nil {
			return fmt.Errorf("%s: %w", *npath, err)
		}
		notes = &n
	}
	// One notes file serves both kinds: frontend finding IDs start with "fe-".
	backendNotes, frontendNotes := splitNotes(notes)

	res := issuesOut{Issues: []issues.Issue{}, Questions: []string{}, NeedsNotes: []string{}}
	if *rpath != "" {
		var run model.Run
		if err := readJSON(*rpath, &run); err != nil {
			return err
		}
		s, err := optionalScan(*spath)
		if err != nil {
			return err
		}
		if backendNotes != nil {
			if err := issues.ValidateNotes(*backendNotes, run, s); err != nil {
				return err
			}
		}
		built, err := issues.Build(run, s, backendNotes, *mr, *starter)
		if err != nil {
			return err
		}
		res.Issues = append(res.Issues, built...)
		if q := issues.Questions(run); q != nil {
			res.Questions = q
		}
		for _, h := range issues.Headlines(run, s) {
			if backendNotes == nil {
				res.NeedsNotes = append(res.NeedsNotes, h.ID)
			} else if _, ok := backendNotes.Items[h.ID]; !ok {
				res.NeedsNotes = append(res.NeedsNotes, h.ID)
			}
		}
	} else if backendNotes != nil && len(backendNotes.Items) > 0 {
		return errors.New("notes: backend finding IDs need --run")
	}
	if *fpath != "" {
		var f model.Frontend
		if err := readJSON(*fpath, &f); err != nil {
			return err
		}
		if frontendNotes != nil {
			if err := issues.ValidateFrontendNotes(*frontendNotes, f); err != nil {
				return err
			}
		}
		fe, err := issues.FrontendIssues(f, frontendNotes, *mr, *starter)
		if err != nil {
			return err
		}
		res.Issues = append(res.Issues, fe...)
		for _, id := range issues.FrontendIDs(f) {
			if frontendNotes == nil {
				res.NeedsNotes = append(res.NeedsNotes, id)
			} else if _, ok := frontendNotes.Items[id]; !ok {
				res.NeedsNotes = append(res.NeedsNotes, id)
			}
		}
	} else if frontendNotes != nil && len(frontendNotes.Items) > 0 {
		return errors.New("notes: frontend finding IDs need --frontend")
	}
	sort.Strings(res.NeedsNotes)
	return writeJSON(*out, res)
}

// splitNotes separates frontend notes (IDs starting "fe-") from backend ones.
func splitNotes(n *model.Notes) (backend, frontend *model.Notes) {
	if n == nil {
		return nil, nil
	}
	b := &model.Notes{Version: n.Version, Items: map[string]model.Note{}}
	f := &model.Notes{Version: n.Version, Items: map[string]model.Note{}}
	for k, v := range n.Items {
		if strings.HasPrefix(k, "fe-") {
			f.Items[k] = v
		} else {
			b.Items[k] = v
		}
	}
	return b, f
}

// cmdFrontend runs the frontend check. It writes frontend.json and exits 0
// whatever the outcome, so a frontend failure never stops the backend report;
// a failed stage shows as withheld. The last stdout line is for the flow.
func cmdFrontend(args []string) error {
	fs := flag.NewFlagSet("frontend", flag.ContinueOnError)
	base := fs.String("base", "", "baseline commit SHA")
	head := fs.String("head", "", "candidate commit SHA")
	jpath := fs.String("journeys", "", "frontend journeys JSON")
	fecheck := fs.String("fecheck", "", "directory holding fecheck.mjs with its node_modules installed")
	repo := fs.String("repo", ".", "repository root")
	out := fs.String("out", "", "output file")
	detect := fs.Bool("detect", false, "only list the changed frontend files and the mode")
	required := []string{"base", "head", "journeys", "fecheck", "out"}
	if slices.Contains(args, "--detect") || slices.Contains(args, "-detect") {
		required = required[:2]
	}
	if err := parse(fs, args, required...); err != nil {
		return err
	}
	if err := commits(*repo, *base, *head); err != nil {
		return err
	}
	if *detect {
		mode, files, err := runner.DetectFrontend(context.Background(), *repo, *base, *head)
		if err != nil {
			return err
		}
		for _, f := range files {
			fmt.Println(f)
		}
		if mode == "" {
			mode = "none"
		}
		fmt.Printf("frontend: files=%d mode=%s\n", len(files), mode)
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	f := runner.Frontend(ctx, runner.FrontendOptions{
		Repo: *repo, Base: *base, Head: *head, Journeys: *jpath, Fecheck: *fecheck, Log: os.Stderr,
	})
	if err := writeJSON(*out, f); err != nil {
		return err
	}
	counts := map[string]int{}
	if f.Compare != nil {
		for _, j := range f.Compare.Journeys {
			for _, x := range j.Findings {
				if x.Metric != "not checked" {
					counts[x.Verdict]++
				}
			}
		}
	}
	fmt.Printf("frontend: status=%s regression=%d improved=%d withheld=%d\n", f.Status, counts["regression"], counts["improved"], counts["withheld"])
	return nil
}

// writeMarkerJourneys writes the journeys a marker carries, in the format the
// re-measure needs: page journeys for a frontend finding, otherwise a
// journeys.json with the headline journey or, for an app-wide finding, all of
// the run's journeys. New-feature journeys become compared ones, because in a
// re-measure the feature exists on both sides.
func writeMarkerJourneys(path string, m model.Marker) error {
	if m.Kind == model.MarkerFrontend {
		if len(m.FEJourneys) == 0 || !json.Valid(m.FEJourneys) {
			return errors.New("the frontend marker holds no page journeys")
		}
		return os.WriteFile(path, m.FEJourneys, 0o644)
	}
	js := m.Journeys
	if m.Journey.ID != "" {
		js = []model.Journey{m.Journey}
	}
	if len(js) == 0 {
		return errors.New("the marker holds no journey; rerun with the run's journeys.json")
	}
	out := make([]model.Journey, len(js))
	for i, j := range js {
		if j.Kind == model.KindNew {
			j.Kind = ""
		}
		out[i] = j
	}
	return writeJSON(path, model.JourneyFile{Version: model.FormatVersion, Journeys: out})
}

func cmdMarker(args []string) error {
	if len(args) < 1 || args[0] != "parse" {
		return errors.New("usage: downstream marker parse [--journeys-out journeys.json] < issue_body.md")
	}
	fs := flag.NewFlagSet("marker parse", flag.ContinueOnError)
	jout := fs.String("journeys-out", "", "write the embedded journey as a journeys.json")
	if err := parse(fs, args[1:]); err != nil {
		return err
	}
	body, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil {
		return err
	}
	m, err := issues.ParseMarker(string(body))
	if err != nil {
		return err
	}
	if *jout != "" {
		if err := writeMarkerJourneys(*jout, m); err != nil {
			return err
		}
	}
	return json.NewEncoder(os.Stdout).Encode(m)
}

func cmdFixcheck(args []string) error {
	fs := flag.NewFlagSet("fixcheck", flag.ContinueOnError)
	mpath := fs.String("marker", "", "marker JSON, or an issue body containing the marker")
	option := fs.Int("option", 0, "option number")
	if err := parse(fs, args, "marker", "option"); err != nil {
		return err
	}
	data, err := os.ReadFile(*mpath)
	if err != nil {
		return err
	}
	m, err := issues.ParseMarker(string(data))
	if err != nil {
		return err
	}
	text, err := issues.FixCheck(m, *option)
	if err != nil {
		return err
	}
	fmt.Printf("option %d of %d for %s: %s\n", *option, len(m.FixOptions), m.FindingID, text)
	return nil
}

// parse parses flags and checks that each required flag was given.
func parse(fs *flag.FlagSet, args []string, required ...string) error {
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	for _, name := range required {
		if !set[name] {
			return fmt.Errorf("--%s is required", name)
		}
	}
	return nil
}

// commits checks that both arguments are hex SHAs and that the commits are in
// the local repository, fetching them from origin when the clone is partial.
func commits(repo string, shas ...string) error {
	for _, sha := range shas {
		if !shaPattern.MatchString(sha) {
			return fmt.Errorf("%q is not a commit SHA (7 to 40 hex characters)", sha)
		}
		if git(repo, "cat-file", "-e", sha+"^{commit}") == nil {
			continue
		}
		if err := git(repo, "fetch", "--quiet", "--no-tags", "--filter=blob:none", "origin", sha); err != nil {
			return fmt.Errorf("commit %s is not in the repository and could not be fetched: %w", sha, err)
		}
	}
	return nil
}

func git(repo string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return nil
}

func readJourneys(path string) (model.JourneyFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.JourneyFile{}, err
	}
	f, err := journey.Parse(data)
	if err != nil {
		return f, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}

func optionalScan(path string) (*model.Scan, error) {
	if path == "" {
		return nil, nil
	}
	var s model.Scan
	if err := readJSON(path, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
