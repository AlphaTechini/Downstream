package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"downstream/engine/history"
	"downstream/engine/model"
	"downstream/engine/target"
)

// cmdHistory dispatches `history record` and `history trend`. Both exit 0 when
// history is off or the memory service is unreachable, so the backend report
// is never blocked by them.
func cmdHistory(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: downstream history record|trend ...")
	}
	switch args[0] {
	case "record":
		return historyRecord(args[1:])
	case "trend":
		return historyTrend(args[1:])
	}
	return fmt.Errorf("unknown history command %q", args[0])
}

// historyRecord writes this run's record when downstream.yml at the base turns
// history on, and writes nothing otherwise.
func historyRecord(args []string) error {
	fs := flag.NewFlagSet("history record", flag.ContinueOnError)
	rpath := fs.String("run", "", "run.json")
	mr := fs.Int("mr", 0, "merge request IID")
	project := fs.String("project", "", "GitLab project path (default: CI_PROJECT_PATH, then the origin remote)")
	repo := fs.String("repo", ".", "repository root")
	out := fs.String("out", "", "output file")
	if err := parse(fs, args, "run", "mr", "out"); err != nil {
		return err
	}
	var run model.Run
	if err := readJSON(*rpath, &run); err != nil {
		return err
	}
	if run.Base == "" {
		fmt.Println("history: skipped (the run did not resolve its commits)")
		return nil
	}
	cfg, err := target.Load(*repo, run.Base)
	if err != nil {
		return err
	}
	if !cfg.History {
		fmt.Println("history: off")
		return nil
	}
	p := *project
	if p == "" {
		p = projectPath(*repo)
	}
	if p == "" {
		fmt.Println("history: skipped (project path unknown)")
		return nil
	}
	e, err := history.Summarize(run, p, *mr)
	if err != nil {
		fmt.Println("history: skipped (" + err.Error() + ")")
		return nil
	}
	if err := writeJSON(*out, e); err != nil {
		return err
	}
	fmt.Printf("history: on, %d metrics recorded for %s !%d\n", len(e.Metrics), p, *mr)
	return nil
}

// projectPath finds the GitLab project path: GitLab sets CI_PROJECT_PATH in
// the job; otherwise it is read from the origin remote, without credentials.
func projectPath(repo string) string {
	if p := os.Getenv("CI_PROJECT_PATH"); p != "" {
		return p
	}
	out, err := exec.Command("git", "-C", repo, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	raw := strings.TrimSpace(string(out))
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
	}
	if _, path, ok := strings.Cut(raw, ":"); ok && strings.Contains(raw, "@") {
		return strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	}
	return ""
}

// historyTrend compounds the merged records the memory service returned with
// this run's record. A missing or unreadable response gives an "unavailable"
// trend, which the report states in one line.
func historyTrend(args []string) error {
	fs := flag.NewFlagSet("history trend", flag.ContinueOnError)
	rec := fs.String("record", "", "this run's record (history record --out)")
	resp := fs.String("response", "", "the memory service's response")
	service := fs.String("service", "", "the memory service's URL, shown in the report")
	repo := fs.String("repo", ".", "repository root")
	out := fs.String("out", "", "output file")
	if err := parse(fs, args, "record", "response", "out"); err != nil {
		return err
	}
	var cur history.Entry
	if err := readJSON(*rec, &cur); err != nil {
		return err
	}
	var r struct {
		Recorded history.Entry   `json:"recorded"`
		Prior    []history.Entry `json:"prior"`
	}
	t := history.Trend{Version: history.Version, Status: "unavailable", Project: cur.Project, Window: history.Window, Limit: history.CreepLimit, Service: *service}
	b, err := os.ReadFile(*resp)
	switch {
	case err != nil:
		t.Reason = "the memory service did not answer"
	case json.Unmarshal(b, &r) != nil || r.Recorded.Head != cur.Head:
		t.Reason = "the memory service's answer was not readable"
	default:
		t = history.Compute(cur, r.Prior, func(head string) bool { return mergedInto(*repo, head, cur.Base) })
		t.Service = *service
	}
	if err := writeJSON(*out, t); err != nil {
		return err
	}
	creeping := 0
	for _, l := range t.Lines {
		if l.Creeping {
			creeping++
		}
	}
	fmt.Printf("trend: status=%s metrics=%d creeping=%d\n", t.Status, len(t.Lines), creeping)
	return nil
}

// mergedInto reports whether head is an ancestor of base, fetching head when
// the clone lacks it. Any failure counts as not merged.
func mergedInto(repo, head, base string) bool {
	if commits(repo, head) != nil {
		return false
	}
	return exec.Command("git", "-C", repo, "merge-base", "--is-ancestor", head, base).Run() == nil
}
