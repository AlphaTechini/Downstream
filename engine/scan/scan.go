// Package scan finds what a diff changed and which routes can reach it.
//
// The analysis is syntactic: go/parser and go/ast, no type checker. Calls are
// matched by name, which over-approximates, and the Method string says so.
package scan

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"downstream/engine/model"
)

// Options selects the diff and the directory to analyse.
type Options struct {
	Repo, Base, Head, Root string // Root is relative to the repo, e.g. examples/shelf
}

// Method is written into scan.json so nobody reads the blast radius as complete.
const Method = "static analysis with go/ast, calls resolved by name, depth 2 from handlers and middleware, a function reached by more than half of all routes is ignored when looking for shared functions; misses dynamic registration, reflection, interface dispatch by other names, and cross-service calls"

const (
	depthLimit = 2
	maxReasons = 5
)

// Run diffs Base against Head and returns the changed functions and the routes
// they reach.
func Run(opts Options) (model.Scan, error) {
	var zero model.Scan
	repo := opts.Repo
	if repo == "" {
		repo = "."
	}
	root, err := cleanRoot(opts.Root)
	if err != nil {
		return zero, err
	}
	base, err := resolveCommit(repo, opts.Base)
	if err != nil {
		return zero, fmt.Errorf("base: %w", err)
	}
	head, err := resolveCommit(repo, opts.Head)
	if err != nil {
		return zero, fmt.Errorf("head: %w", err)
	}

	diffOut, err := git(repo, "diff", "--no-color", "--no-ext-diff", "--no-textconv", "-U0", "-M",
		"--src-prefix=a/", "--dst-prefix=b/", base, head, "--", root)
	if err != nil {
		return zero, err
	}
	files, err := parseDiff(string(diffOut))
	if err != nil {
		return zero, fmt.Errorf("parse diff: %w", err)
	}

	sc := model.Scan{
		Version:      model.FormatVersion,
		Base:         base,
		Head:         head,
		Root:         root,
		Method:       Method,
		ChangedFiles: []model.ChangedFile{},
		ChangedFuncs: []string{},
		Routes:       []model.Route{},
		Changed:      []model.Impact{},
		Affected:     []model.Impact{},
	}
	hunks := map[string][]model.Hunk{}
	goChanged := 0
	for _, f := range files {
		sc.ChangedFiles = append(sc.ChangedFiles, f.ChangedFile)
		if strings.HasSuffix(f.Path, ".go") && !strings.HasSuffix(f.Path, "_test.go") {
			goChanged++
			if f.Status != statusDeleted {
				hunks[f.Path] = f.Hunks
			}
		}
	}
	if goChanged == 0 {
		sc.Notes = append(sc.Notes, fmt.Sprintf("no Go files changed under %s", root))
	}
	if n, err := outsideRoot(repo, base, head, root); err != nil {
		return zero, err
	} else if n > 0 {
		sc.Notes = append(sc.Notes, fmt.Sprintf("diff touches files outside %s: %d", root, n))
	}
	sc.Notes = append(sc.Notes, deletedNotes(repo, base, files)...)

	if !existsInCommit(repo, treeish(head, root)) {
		sc.Notes = append(sc.Notes, fmt.Sprintf("%s does not exist in the head commit, so there are no routes", root))
		return sc, nil
	}
	tmp, err := os.MkdirTemp("", "downstream-scan-")
	if err != nil {
		return zero, err
	}
	defer os.RemoveAll(tmp)
	if err := extractGo(repo, treeish(head, root), tmp); err != nil {
		return zero, err
	}
	cfg := loadConfig{dir: tmp, prefix: root}
	cfg.module, cfg.modBase = moduleFromGit(repo, head, root)
	prog, err := load(cfg)
	if err != nil {
		return zero, err
	}
	sc.Notes = append(sc.Notes, prog.notes...)
	for _, r := range prog.routes {
		sc.Routes = append(sc.Routes, r.Route)
	}
	if len(prog.routes) == 0 {
		sc.Notes = append(sc.Notes, fmt.Sprintf("no HandleFunc or Handle registrations with a literal pattern found under %s", root))
	}

	changed := prog.changedFuncs(hunks)
	for _, f := range changed {
		sc.ChangedFuncs = append(sc.ChangedFuncs, f.key)
	}
	sort.Strings(sc.ChangedFuncs)
	prog.blastRadius(&sc, changed)
	return sc, nil
}

// outsideRoot counts files in the whole diff that are not under root.
func outsideRoot(repo, base, head, root string) (int, error) {
	if root == "." {
		return 0, nil
	}
	out, err := git(repo, "diff", "--name-only", "-z", "--no-ext-diff", base, head)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, name := range strings.Split(string(out), "\x00") {
		if name == "" {
			continue
		}
		if name != root && !strings.HasPrefix(name, root+"/") {
			n++
		}
	}
	return n, nil
}

func touches(hs []model.Hunk, start, end int) bool {
	for _, h := range hs {
		first, last := h.NewStart, h.NewStart+h.NewLines-1
		if h.NewLines == 0 {
			last = first // a deletion touches the line it follows
		}
		if first <= end && last >= start {
			return true
		}
	}
	return false
}

// changedFuncs lists functions whose lines overlap a hunk, plus functions that
// reference a declaration (const, var, type) whose lines overlap a hunk.
func (p *program) changedFuncs(hunks map[string][]model.Hunk) []*function {
	changedDecl := map[string]bool{}
	for _, d := range p.declKey {
		if touches(hunks[d.file], d.start, d.end) {
			changedDecl[d.key] = true
		}
	}
	var out []*function
	for _, f := range p.funcs {
		changedHere := touches(hunks[f.file], f.start, f.end)
		for _, k := range f.uses {
			changedHere = changedHere || changedDecl[k]
		}
		for _, k := range f.recvUses {
			changedHere = changedHere || changedDecl[k]
		}
		if changedHere {
			out = append(out, f)
		}
	}
	return out
}

// reach returns f and its callees to the given depth, with the shortest depth.
func reach(roots []*function, depth int) map[*function]int {
	seen := map[*function]int{}
	frontier := append([]*function(nil), roots...)
	for _, r := range roots {
		seen[r] = 0
	}
	for d := 1; d <= depth; d++ {
		var next []*function
		for _, f := range frontier {
			for _, c := range f.calls {
				if _, ok := seen[c]; !ok {
					seen[c] = d
					next = append(next, c)
				}
			}
		}
		frontier = next
	}
	return seen
}

type hit struct {
	f     *function
	depth int
}

func hitsIn(r map[*function]int, set map[*function]bool) []hit {
	var hs []hit
	for f, d := range r {
		if set[f] {
			hs = append(hs, hit{f, d})
		}
	}
	sort.Slice(hs, func(i, j int) bool {
		if hs[i].depth != hs[j].depth {
			return hs[i].depth < hs[j].depth
		}
		return hs[i].f.key < hs[j].f.key
	})
	return hs
}

type routeReach struct {
	ri       *routeInfo
	handler  map[*function]int
	all      map[*function]int // handler and middleware paths together
	impact   model.Impact
	isChange bool
}

func (p *program) blastRadius(sc *model.Scan, changed []*function) {
	changedSet := map[*function]bool{}
	for _, f := range changed {
		changedSet[f] = true
	}
	var rrs []*routeReach
	for _, ri := range p.routes {
		rr := &routeReach{ri: ri, all: map[*function]int{}}
		rr.handler = reach(ri.handlerRoots, depthLimit)
		merge(rr.all, rr.handler)
		var reasons []string
		onPath := map[string]bool{}
		for _, h := range hitsIn(rr.handler, changedSet) {
			onPath[h.f.key] = true
			if h.depth == 0 {
				reasons = append(reasons, fmt.Sprintf("handler %s itself changed", ri.Handler))
			} else {
				reasons = append(reasons, fmt.Sprintf("handler %s calls changed function %s (depth %d)", ri.Handler, h.f.key, h.depth))
			}
		}
		for _, m := range ri.mws {
			mr := reach(m.roots, depthLimit)
			merge(rr.all, mr)
			for _, h := range hitsIn(mr, changedSet) {
				onPath[h.f.key] = true
				if h.depth == 0 {
					reasons = append(reasons, fmt.Sprintf("middleware %s itself changed: every request on this route runs it", m.name))
				} else {
					reasons = append(reasons, fmt.Sprintf("middleware %s reaches changed function %s (depth %d): every request on this route runs it", m.name, h.f.key, h.depth))
				}
			}
		}
		if len(onPath) > 0 {
			rr.isChange = true
			rr.impact = model.Impact{Route: ri.Route, Reasons: capReasons(reasons), ChangedFuncs: sortedKeys(onPath)}
		}
		rrs = append(rrs, rr)
	}

	usersOfDecl := map[string][]*function{} // changed functions by declaration they use
	for _, f := range changed {
		for _, k := range f.uses {
			usersOfDecl[k] = append(usersOfDecl[k], f)
		}
	}
	// A function that most routes reach says nothing about which routes depend
	// on each other, so it is left out of the shared-function rule. It still
	// makes every route that reaches it Changed when it changes.
	reachedBy := map[*function]int{}
	for _, rr := range rrs {
		for f := range rr.all {
			reachedBy[f]++
		}
	}
	ubiquitous := map[*function]bool{}
	for f, n := range reachedBy {
		if n*2 > len(rrs) {
			ubiquitous[f] = true
		}
	}
	for _, rr := range rrs {
		if rr.isChange {
			sc.Changed = append(sc.Changed, rr.impact)
			continue
		}
		if reasons := p.affectedReasons(rr, rrs, usersOfDecl, ubiquitous); len(reasons) > 0 {
			sc.Affected = append(sc.Affected, model.Impact{Route: rr.ri.Route, Reasons: reasons})
		}
	}
}

func merge(dst, src map[*function]int) {
	for f, d := range src {
		if old, ok := dst[f]; !ok || d < old {
			dst[f] = d
		}
	}
}

func capReasons(r []string) []string {
	if len(r) > maxReasons {
		r = r[:maxReasons]
	}
	return r
}

// affectedReasons says why a route that reaches no changed function still
// shares something with a changed route: a function on both paths, or a
// package-level type or variable that a changed function also uses.
func (p *program) affectedReasons(rr *routeReach, all []*routeReach, usersOfDecl map[string][]*function, ubiquitous map[*function]bool) []string {
	type shared struct {
		f     *function
		route string
		score int
	}
	best := map[*function]shared{}
	for _, other := range all {
		if !other.isChange {
			continue
		}
		for f, d := range rr.all {
			if d2, ok := other.all[f]; ok && !ubiquitous[f] {
				s := shared{f: f, route: label(other.ri.Route), score: d + d2}
				if old, seen := best[f]; !seen || s.score < old.score {
					best[f] = s
				}
			}
		}
	}
	var fs []shared
	for _, s := range best {
		fs = append(fs, s)
	}
	sort.Slice(fs, func(i, j int) bool {
		if fs[i].score != fs[j].score {
			return fs[i].score < fs[j].score
		}
		return fs[i].f.key < fs[j].f.key
	})
	var funcReasons []string
	for _, s := range fs {
		funcReasons = append(funcReasons, fmt.Sprintf("shares function %s with changed route %s", s.f.key, s.route))
	}

	seenDecl := map[string]bool{}
	var declReasons []string
	var funcsOnRoute []*function
	for f := range rr.all {
		funcsOnRoute = append(funcsOnRoute, f)
	}
	sort.Slice(funcsOnRoute, func(i, j int) bool { return funcsOnRoute[i].key < funcsOnRoute[j].key })
	for _, f := range funcsOnRoute {
		for _, k := range f.uses {
			d := p.declKey[k]
			if d == nil || seenDecl[k] || (d.kind != "type" && d.kind != "var") || len(usersOfDecl[k]) == 0 {
				continue
			}
			seenDecl[k] = true
			cf := usersOfDecl[k][0]
			if d.kind == "type" {
				declReasons = append(declReasons, fmt.Sprintf("uses type %s, which changed function %s also uses", d.name, cf.key))
			} else {
				declReasons = append(declReasons, fmt.Sprintf("shares package variable %s with changed function %s", d.name, cf.key))
			}
		}
	}

	var out []string
	take := func(src []string, n int) []string {
		if n > len(src) {
			n = len(src)
		}
		return src[:n]
	}
	out = append(out, take(funcReasons, 3)...)
	out = append(out, take(declReasons, 2)...)
	if len(funcReasons) > 3 {
		out = append(out, funcReasons[3:]...)
	}
	if len(declReasons) > 2 {
		out = append(out, declReasons[2:]...)
	}
	return capReasons(out)
}

// deletedNotes names the functions of deleted Go files. They are not in the
// head build, so they are reported here and not traced.
func deletedNotes(repo, base string, files []fileDiff) []string {
	var notes []string
	for _, f := range files {
		if f.Status != statusDeleted || !strings.HasSuffix(f.Path, ".go") || strings.HasSuffix(f.Path, "_test.go") {
			continue
		}
		out, err := git(repo, "cat-file", "blob", base+":"+f.Path)
		if err != nil {
			continue
		}
		names := declaredFuncs(f.Path, out)
		if len(names) == 0 {
			notes = append(notes, fmt.Sprintf("%s was deleted; it declared no functions", f.Path))
			continue
		}
		more := ""
		if len(names) > 10 {
			more = fmt.Sprintf(" and %d more", len(names)-10)
			names = names[:10]
		}
		notes = append(notes, fmt.Sprintf("%s was deleted; it declared %s%s. They are not in the head build and were not traced", f.Path, strings.Join(names, ", "), more))
	}
	return notes
}
