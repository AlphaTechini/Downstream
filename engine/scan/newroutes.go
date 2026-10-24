package scan

import (
	"fmt"
	"os"
	"unicode/utf8"

	"downstream/engine/model"
)

// maxAppNotes caps downstream.md so a large file cannot crowd out the scan.
const maxAppNotes = 8 << 10

// routesAt parses the route table of root at commit. ok is false when root does
// not exist there, as for a service the merge request adds.
func routesAt(repo, commit, root string) (routes []model.Route, ok bool, err error) {
	if !existsInCommit(repo, treeish(commit, root)) {
		return nil, false, nil
	}
	tmp, err := os.MkdirTemp("", "downstream-scan-base-")
	if err != nil {
		return nil, false, err
	}
	defer os.RemoveAll(tmp)
	if err := extractGo(repo, treeish(commit, root), tmp); err != nil {
		return nil, false, err
	}
	cfg := loadConfig{dir: tmp, prefix: root}
	cfg.module, cfg.modBase = moduleFromGit(repo, commit, root)
	prog, err := load(cfg)
	if err != nil {
		return nil, false, err
	}
	for _, r := range prog.routes {
		routes = append(routes, r.Route)
	}
	return routes, true, nil
}

// newRoutes returns the head routes whose method and pattern the base lacks.
func newRoutes(head, base []model.Route) []model.Route {
	seen := map[string]bool{}
	for _, r := range base {
		seen[r.Method+" "+r.Path] = true
	}
	out := []model.Route{}
	for _, r := range head {
		if !seen[r.Method+" "+r.Path] {
			out = append(out, r)
		}
	}
	return out
}

// appNotes reads downstream.md from the base commit: what the repo wants the
// journey writer to know, such as seeded test data and its key pages. Reading
// it from the base keeps a merge request from rewriting its own instructions.
func appNotes(repo, base string) (string, string) {
	if !existsInCommit(repo, base+":downstream.md") {
		return "", ""
	}
	b, err := git(repo, "show", base+":downstream.md")
	if err != nil {
		return "", fmt.Sprintf("downstream.md could not be read: %v", err)
	}
	if len(b) > maxAppNotes {
		cut := maxAppNotes
		for cut > 0 && !utf8.RuneStart(b[cut]) {
			cut--
		}
		return string(b[:cut]), fmt.Sprintf("downstream.md is longer than %d bytes and was cut", maxAppNotes)
	}
	return string(b), ""
}
