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
	"regexp"
	"runtime"
	"sort"
	"strings"
	"syscall"

	"downstream/engine/classify"
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
  downstream report   --run run.json [--scan scan.json] --out report.md
  downstream summary  --run run.json --scan scan.json
  downstream issues   --run run.json [--scan scan.json] [--notes notes.json] --mr <iid> --starter <username> --out issues.json
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
		"issues":   cmdIssues,
		"marker":   cmdMarker,
		"fixcheck": cmdFixcheck,
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
	f, err := readJourneys(*jpath)
	if err != nil {
		return err
	}
	var s model.Scan
	if err := readJSON(*spath, &s); err != nil {
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
	fmt.Printf("ok: %d journeys (%d existing, %d new, %d control), %d steps\n",
		len(f.Journeys), kinds[model.KindExisting], kinds[model.KindNew], kinds[model.KindControl], steps)
	for _, r := range uncovered {
		fmt.Printf("uncovered: %s %s (journey cap reached)\n", r.Method, r.Path)
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
	out := fs.String("out", "", "output file")
	if err := parse(fs, args, "run", "out"); err != nil {
		return err
	}
	var run model.Run
	if err := readJSON(*rpath, &run); err != nil {
		return err
	}
	s, err := optionalScan(*spath)
	if err != nil {
		return err
	}
	return os.WriteFile(*out, []byte(report.Render(run, s)), 0o644)
}

// cmdSummary prints one count per line so the flow and CI can branch on it
// without reading the report.
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
	counts := map[string]int{}
	for _, f := range run.Findings {
		counts[f.Class]++
	}
	tradeJourneys := map[string]bool{}
	for _, f := range run.Findings {
		if f.Class == model.ClassTradeOff {
			tradeJourneys[f.Journey] = true
		}
	}
	fmt.Printf("regression=%d\n", len(issues.Headlines(run, scan)))
	fmt.Printf("trade-off=%d\n", len(tradeJourneys))
	fmt.Printf("improved=%d\n", counts[model.ClassImproved])
	fmt.Printf("withheld=%d\n", counts[model.ClassWithheld])
	fmt.Printf("discarded=%d\n", len(run.Discarded))
	fmt.Printf("passes=%d\n", len(run.Passes))
	fmt.Printf("new_failures=%d\n", report.NewFailures(run))
	if run.Error != nil {
		fmt.Printf("stopped=%s\n", run.Error.Stage)
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
	mr := fs.Int("mr", 0, "merge request IID")
	starter := fs.String("starter", "", "username of whoever started the run")
	out := fs.String("out", "", "output file")
	if err := parse(fs, args, "run", "mr", "starter", "out"); err != nil {
		return err
	}
	var run model.Run
	if err := readJSON(*rpath, &run); err != nil {
		return err
	}
	s, err := optionalScan(*spath)
	if err != nil {
		return err
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
		if err := issues.ValidateNotes(n, run, s); err != nil {
			return err
		}
		notes = &n
	}
	built, err := issues.Build(run, s, notes, *mr, *starter)
	if err != nil {
		return err
	}
	res := issuesOut{Issues: built, Questions: issues.Questions(run), NeedsNotes: []string{}}
	if res.Issues == nil {
		res.Issues = []issues.Issue{}
	}
	if res.Questions == nil {
		res.Questions = []string{}
	}
	for _, h := range issues.Headlines(run, s) {
		if notes == nil {
			res.NeedsNotes = append(res.NeedsNotes, h.ID)
		} else if _, ok := notes.Items[h.ID]; !ok {
			res.NeedsNotes = append(res.NeedsNotes, h.ID)
		}
	}
	sort.Strings(res.NeedsNotes)
	return writeJSON(*out, res)
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
		if m.Journey.ID == "" {
			return errors.New("the marker holds no journey (app-wide finding); rerun with the run's journeys.json")
		}
		f := model.JourneyFile{Version: model.FormatVersion, Journeys: []model.Journey{m.Journey}}
		if err := writeJSON(*jout, f); err != nil {
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
