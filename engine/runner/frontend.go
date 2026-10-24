package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"downstream/engine/model"
	"downstream/engine/target"
)

// frontendBudget caps the whole frontend stage so it fits the flow's 200 s
// command limit: installs, two builds and three page collections.
const frontendBudget = 190 * time.Second

const collectTimeout = 90 * time.Second

// FrontendOptions configure one frontend check.
type FrontendOptions struct {
	Repo, Base, Head string
	Journeys         string    // frontend journeys JSON, validated by fecheck before use
	Fecheck          string    // directory holding fecheck.mjs and its installed node_modules
	Log              io.Writer // one progress line per stage; nil discards
}

// Frontend builds base and head, serves each on loopback, collects the
// journeys' pages with fecheck (base twice, to prove the metrics are stable)
// and compares them. It always returns a result: a stage that fails makes the
// status withheld with the stage and reason, and nothing is claimed.
func Frontend(ctx context.Context, o FrontendOptions) model.Frontend {
	res := model.Frontend{
		Version:    model.FrontendFormatVersion,
		NotChecked: model.FENotChecked,
		StartedAt:  stamp(time.Now()),
	}
	ctx, cancel := context.WithTimeout(ctx, frontendBudget)
	defer cancel()
	if err := frontend(ctx, o, &res); err != nil {
		res.Status = model.FrontendWithheld
		var se *stageError
		if errors.As(err, &se) {
			res.FailedStage, res.FailedMessage = se.stage, se.err.Error()
		} else {
			res.FailedStage, res.FailedMessage = "frontend", err.Error()
		}
		if ctx.Err() != nil {
			res.FailedMessage += fmt.Sprintf(" (the frontend stage has %s in all)", frontendBudget)
		}
		res.Reason = "the frontend check could not finish, so no frontend result is claimed"
		res.Compare = nil
	}
	res.EndedAt = stamp(time.Now())
	return res
}

// side is one built and served commit.
type side struct {
	name, sha, tree string
	origin          string
	stop            []func()
}

func frontend(ctx context.Context, o FrontendOptions, res *model.Frontend) error {
	log := logger{o.Log}
	baseSHA, err := resolveCommit(ctx, o.Repo, o.Base)
	if err != nil {
		return fail("resolve commits", err)
	}
	headSHA, err := resolveCommit(ctx, o.Repo, o.Head)
	if err != nil {
		return fail("resolve commits", err)
	}
	res.Base, res.Head = baseSHA, headSHA
	cfg, err := target.Load(o.Repo, baseSHA)
	if err != nil {
		return fail("load config", err)
	}
	files, err := changedFiles(ctx, o.Repo, baseSHA, headSHA)
	if err != nil {
		return fail("changed files", err)
	}
	res.ChangedFiles = frontendChanges(cfg, files)
	if len(res.ChangedFiles) == 0 {
		res.Status = model.FrontendSkipped
		res.Reason = "the change touches no frontend file"
		log.printf("frontend: skipped, no frontend file changed")
		return nil
	}
	switch {
	case cfg.HasStaticFrontend():
		res.Mode = "static"
	case cfg.HasBackend():
		res.Mode = "server"
	default:
		return fail("load config", errors.New("downstream.yml declares neither a frontend build nor a backend"))
	}

	tmp, err := os.MkdirTemp("", "downstream-fe-")
	if err != nil {
		return fail("temp dir", err)
	}
	defer os.RemoveAll(tmp)

	env := os.Environ()
	nodePath, err := exec.LookPath("node")
	if want := cfg.Frontend.Node; want != "" {
		if have, verr := nodeVersion(ctx, env); verr != nil || have != want {
			log.printf("frontend: fetching node %s", want)
			bin, ferr := fetchNode(ctx, want, filepath.Join(tmp, "node"))
			if ferr != nil {
				return fail("node", ferr)
			}
			env = withPath(env, bin)
			nodePath, err = filepath.Join(bin, "node"), nil
		}
	}
	if err != nil {
		return fail("node", errors.New("node was not found on PATH"))
	}
	if res.NodeVersion, err = nodeVersion(ctx, env); err != nil {
		return fail("node", err)
	}
	fecheck := func(args ...string) ([]byte, error) {
		return runFecheck(ctx, nodePath, o.Fecheck, env, args...)
	}

	journeys := filepath.Join(tmp, "fe-journeys.json")
	out, err := fecheck("validate", "--journeys", o.Journeys, "--out", journeys)
	if err != nil {
		return fail("validate journeys", err)
	}
	var v struct {
		Journeys int      `json:"journeys"`
		Dropped  []string `json:"dropped"`
	}
	if err := json.Unmarshal(lastJSONLine(out), &v); err != nil {
		return fail("validate journeys", fmt.Errorf("unexpected validator output: %w", err))
	}
	res.Journeys, res.Dropped = v.Journeys, v.Dropped
	log.printf("frontend: %d journeys, %d steps dropped by validation", v.Journeys, len(v.Dropped))

	sides := []*side{{name: "base", sha: baseSHA}, {name: "head", sha: headSHA}}
	defer func() {
		for _, s := range sides {
			for i := len(s.stop) - 1; i >= 0; i-- {
				s.stop[i]()
			}
		}
	}()
	for _, s := range sides {
		if err := serveSide(ctx, o.Repo, cfg, tmp, s, sides[0], env, log); err != nil {
			return err
		}
	}

	collect := func(s *side, name string) (string, error) {
		p := filepath.Join(tmp, name+".json")
		log.printf("frontend: collect %s", name)
		cctx, cancel := context.WithTimeout(ctx, collectTimeout)
		defer cancel()
		if _, err := runFecheck(cctx, nodePath, o.Fecheck, env, "collect", "--url", s.origin, "--journeys", journeys, "--out", p); err != nil {
			return "", fail("collect ("+name+")", err)
		}
		return p, nil
	}
	basePath, err := collect(sides[0], "base")
	if err != nil {
		return err
	}
	base2Path, err := collect(sides[0], "base2")
	if err != nil {
		return err
	}
	headPath, err := collect(sides[1], "head")
	if err != nil {
		return err
	}
	cmpPath := filepath.Join(tmp, "compare.json")
	if _, err := fecheck("compare", "--base", basePath, "--head", headPath, "--base2", base2Path, "--out", cmpPath); err != nil {
		return fail("compare", err)
	}
	var cmp model.FECompare
	if err := readJSONFile(cmpPath, &cmp); err != nil {
		return fail("compare", err)
	}
	res.Compare = &cmp
	res.Status = model.FrontendChecked
	return nil
}

// serveSide extracts, builds and serves one commit and sets s.origin. The head
// reuses the base's node_modules when the two lockfiles are identical.
func serveSide(ctx context.Context, repo string, cfg target.Config, tmp string, s, base *side, env []string, log logger) error {
	stage := "frontend build (" + s.name + ")"
	if s.name == "head" && s.sha == base.sha {
		s.tree, s.origin = base.tree, base.origin
		return nil
	}
	s.tree = filepath.Join(tmp, s.name+"-src")
	if err := os.Mkdir(s.tree, 0o755); err != nil {
		return fail(stage, err)
	}
	log.printf("%s: extracting %s", stage, short(s.sha))
	if err := materialise(ctx, repo, s.sha, s.tree); err != nil {
		return fail(stage, fmt.Errorf("extracting the commit: %w", err))
	}

	var api *url.URL
	if cfg.HasBackend() {
		bin := filepath.Join(tmp, s.name+"-app")
		log.printf("%s: %s", stage, cfg.BuildCommand(bin))
		if err := buildTree(ctx, cfg, s.tree, bin, os.Environ()); err != nil {
			return fail(stage, err)
		}
		port, err := pickPort()
		if err != nil {
			return fail(stage, err)
		}
		a, err := startApp(cfg, bin, s.tree, port)
		if err != nil {
			return fail(stage, err)
		}
		s.stop = append(s.stop, a.stop)
		if err := a.waitReady(ctx, cfg.ReadyPath); err != nil {
			return fail(stage, err)
		}
		if !cfg.HasStaticFrontend() {
			s.origin = a.baseURL()
			return nil
		}
		api, _ = url.Parse(a.baseURL())
	}

	dir := filepath.Join(s.tree, filepath.FromSlash(cfg.Frontend.Dir))
	baseDir := filepath.Join(base.tree, filepath.FromSlash(cfg.Frontend.Dir))
	_, nmErr := os.Stat(filepath.Join(baseDir, "node_modules"))
	if s.name == "head" && nmErr == nil && sameFile(filepath.Join(baseDir, "package-lock.json"), filepath.Join(dir, "package-lock.json")) {
		log.printf("%s: same lockfile as base, linking node_modules", stage)
		if err := linkTree(filepath.Join(baseDir, "node_modules"), filepath.Join(dir, "node_modules")); err != nil {
			return fail(stage, fmt.Errorf("linking node_modules: %w", err))
		}
	} else {
		log.printf("%s: npm ci", stage)
		if err := installDeps(ctx, dir, env); err != nil {
			return fail(stage, err)
		}
	}
	log.printf("%s: %s", stage, cfg.Frontend.Build)
	if err := shell(ctx, dir, cfg.Frontend.Build, env); err != nil {
		return fail(stage, err)
	}
	srv, err := startStatic(filepath.Join(s.tree, filepath.FromSlash(cfg.Frontend.Out)), api)
	if err != nil {
		return fail(stage, err)
	}
	s.stop = append(s.stop, srv.stop)
	s.origin = srv.baseURL()
	return nil
}

// runFecheck runs one fecheck command and returns its stdout. Errors carry the
// tail of its output.
func runFecheck(ctx context.Context, node, dir string, env []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, node, append([]string{filepath.Join(dir, "fecheck.mjs")}, args...)...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.WaitDelay = 5 * time.Second
	stderr := &tailBuffer{max: 4096}
	cmd.Stderr = stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("fecheck %s: %w; output tail:\n%s", args[0], err, stderr.String())
	}
	return out, nil
}

// lastJSONLine returns the last line of out, where fecheck prints its summary.
func lastJSONLine(out []byte) []byte {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return []byte(lines[len(lines)-1])
}

func readJSONFile(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
