package scan

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// function is a declared function or method, or a function literal that is
// registered directly as a route handler.
type function struct {
	key, name, recv, dir, file string
	start, end                 int
	fi                         *fileInfo
	recvName, recvType         string // receiver variable and type of the enclosing method
	node                       ast.Node
	calls                      []*function
	uses                       []string // keys of same-package declarations referenced, receiver excluded
	recvUses                   []string // the receiver type, if it is a declaration of the package
}

// decl is a top-level const, var or type.
type decl struct {
	key, name, kind, dir, file string
	start, end                 int
}

type fileInfo struct {
	path    string // as reported in routes: prefix + path relative to the scanned directory
	dir     string // package directory relative to the scanned directory, "." for the top
	file    *ast.File
	imports map[string]string // local import name -> package dir, "" for packages outside the tree
}

type loadConfig struct {
	dir     string // directory on disk to scan
	prefix  string // path that dir has in the repo, used for file names
	module  string // module path from go.mod, if found
	modBase string // dir relative to the module root
}

type program struct {
	cfg     loadConfig
	fset    *token.FileSet
	files   []*fileInfo
	pkgName map[string]string
	funcs   []*function
	byKey   map[string]*function
	plain   map[string]map[string]*function // dir -> name -> function
	methods map[string][]*function          // method name -> every method with it
	decls   map[string]map[string]*decl     // dir -> name -> declaration
	declKey map[string]*decl
	byNode  map[ast.Node]*function
	routes  []*routeInfo
	notes   []string
}

func skipDirName(name string) bool {
	return name == "testdata" || name == "vendor" || name == "node_modules" ||
		strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

func goSource(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
}

// load parses every Go package under cfg.dir (syntax only, no type checking)
// and builds the function index, declaration index, call edges and routes.
func load(cfg loadConfig) (*program, error) {
	p := &program{
		cfg:     cfg,
		fset:    token.NewFileSet(),
		pkgName: map[string]string{},
		byKey:   map[string]*function{},
		plain:   map[string]map[string]*function{},
		methods: map[string][]*function{},
		decls:   map[string]map[string]*decl{},
		declKey: map[string]*decl{},
		byNode:  map[ast.Node]*function{},
	}
	err := filepath.WalkDir(cfg.dir, func(fp string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(cfg.dir, fp)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if rel != "." && skipDirName(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !goSource(d.Name()) {
			return nil
		}
		src, err := os.ReadFile(fp)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		repoPath := path.Join(cfg.prefix, relSlash)
		f, err := parser.ParseFile(p.fset, repoPath, src, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("parse %s: %w", repoPath, err)
		}
		dir := path.Dir(relSlash)
		p.files = append(p.files, &fileInfo{path: repoPath, dir: dir, file: f})
		if _, ok := p.pkgName[dir]; !ok {
			p.pkgName[dir] = f.Name.Name
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, fi := range p.files {
		p.resolveImports(fi)
	}
	for _, fi := range p.files {
		p.indexFile(fi)
	}
	for _, f := range p.funcs {
		p.collect(f)
	}
	for _, fi := range p.files {
		p.extractRoutes(fi)
	}
	return p, nil
}

var majorVersion = regexp.MustCompile(`^v[0-9]+$`)

func defaultImportName(importPath string) string {
	parts := strings.Split(importPath, "/")
	last := parts[len(parts)-1]
	if majorVersion.MatchString(last) && len(parts) > 1 {
		last = parts[len(parts)-2]
	}
	if i := strings.Index(last, "."); i > 0 {
		last = last[:i]
	}
	return strings.ReplaceAll(last, "-", "_")
}

func (p *program) resolveImports(fi *fileInfo) {
	fi.imports = map[string]string{}
	for _, is := range fi.file.Imports {
		ip, err := strconv.Unquote(is.Path.Value)
		if err != nil {
			continue
		}
		dir, internal := p.resolveImport(ip)
		var name string
		switch {
		case is.Name != nil:
			name = is.Name.Name
		case internal:
			name = p.pkgName[dir]
		default:
			name = defaultImportName(ip)
		}
		if name == "_" || name == "." || name == "" {
			continue
		}
		if internal {
			fi.imports[name] = dir
		} else {
			fi.imports[name] = ""
		}
	}
}

// resolveImport maps an import path to a package directory inside the scanned
// tree. With a known module path the match is exact; without one it falls
// back to the longest path suffix.
func (p *program) resolveImport(ip string) (string, bool) {
	if mod := p.cfg.module; mod != "" {
		var rel string
		switch {
		case ip == mod:
			rel = "."
		case strings.HasPrefix(ip, mod+"/"):
			rel = strings.TrimPrefix(ip, mod+"/")
		default:
			return "", false
		}
		base := p.cfg.modBase
		var dir string
		switch {
		case base == "." || base == "":
			dir = rel
		case rel == base:
			dir = "."
		case strings.HasPrefix(rel, base+"/"):
			dir = strings.TrimPrefix(rel, base+"/")
		default:
			return "", false
		}
		_, ok := p.pkgName[dir]
		return dir, ok
	}
	best, bestLen := "", 0
	for dir := range p.pkgName {
		for _, c := range []string{path.Join(p.cfg.prefix, dir), dir} {
			if c == "." || c == "" {
				continue
			}
			if (ip == c || strings.HasSuffix(ip, "/"+c)) && len(c) > bestLen {
				best, bestLen = dir, len(c)
			}
		}
	}
	return best, bestLen > 0
}

func funcKey(dir, recv, name string) string {
	n := name
	if recv != "" {
		n = "(" + recv + ")." + name
	}
	if dir == "." {
		return n
	}
	return dir + "." + n
}

func declKeyOf(dir, name string) string { return funcKey(dir, "", name) }

func recvTypeName(e ast.Expr) string {
	for {
		switch x := e.(type) {
		case *ast.StarExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}

func (p *program) line(pos token.Pos) int { return p.fset.Position(pos).Line }

func (p *program) addFunc(f *function) {
	if _, dup := p.byKey[f.key]; dup {
		// init functions, or build-tag variants declared twice in one directory
		f.key = fmt.Sprintf("%s@%s:%d", f.key, f.file, f.start)
	}
	p.byKey[f.key] = f
	p.funcs = append(p.funcs, f)
}

func (p *program) indexFile(fi *fileInfo) {
	for _, d := range fi.file.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			f := &function{
				name:  d.Name.Name,
				dir:   fi.dir,
				file:  fi.path,
				start: p.line(d.Pos()),
				end:   p.line(d.End()),
				fi:    fi,
				node:  d,
			}
			if d.Recv != nil && len(d.Recv.List) == 1 {
				f.recv = recvTypeName(d.Recv.List[0].Type)
				f.recvType = f.recv
				if names := d.Recv.List[0].Names; len(names) == 1 {
					f.recvName = names[0].Name
				}
			}
			f.key = funcKey(fi.dir, f.recv, f.name)
			p.addFunc(f)
			p.byNode[d] = f
			if f.recv != "" {
				p.methods[f.name] = append(p.methods[f.name], f)
			} else {
				if p.plain[fi.dir] == nil {
					p.plain[fi.dir] = map[string]*function{}
				}
				if _, dup := p.plain[fi.dir][f.name]; !dup {
					p.plain[fi.dir][f.name] = f
				}
			}
		case *ast.GenDecl:
			var kind string
			switch d.Tok {
			case token.CONST:
				kind = "const"
			case token.VAR:
				kind = "var"
			case token.TYPE:
				kind = "type"
			default:
				continue
			}
			for _, spec := range d.Specs {
				start, end := p.line(spec.Pos()), p.line(spec.End())
				if len(d.Specs) == 1 && !d.Lparen.IsValid() {
					start, end = p.line(d.Pos()), p.line(d.End())
				}
				var names []*ast.Ident
				switch s := spec.(type) {
				case *ast.ValueSpec:
					names = s.Names
				case *ast.TypeSpec:
					names = []*ast.Ident{s.Name}
				}
				for _, n := range names {
					if n.Name == "_" {
						continue
					}
					dc := &decl{key: declKeyOf(fi.dir, n.Name), name: n.Name, kind: kind, dir: fi.dir, file: fi.path, start: start, end: end}
					if p.decls[fi.dir] == nil {
						p.decls[fi.dir] = map[string]*decl{}
					}
					p.decls[fi.dir][n.Name] = dc
					p.declKey[dc.key] = dc
				}
			}
		}
	}
}

// collect fills in call edges and declaration uses for one function. Edges are
// matched by name, so the graph over-approximates: x.M(...) links to every
// method named M in the tree unless x is the receiver variable and the
// receiver type has such a method.
func (p *program) collect(f *function) {
	var body *ast.BlockStmt
	switch n := f.node.(type) {
	case *ast.FuncDecl:
		body = n.Body
	case *ast.FuncLit:
		body = n.Body
	}
	callees := map[*function]bool{}
	var visit func(n ast.Node) bool
	visit = func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok {
				if dir, isImport := f.fi.imports[id.Name]; isImport {
					if dir != "" {
						if g := p.plain[dir][x.Sel.Name]; g != nil {
							callees[g] = true
						}
					}
					return false
				}
				if f.recvName != "" && id.Name == f.recvName {
					found := false
					for _, m := range p.methods[x.Sel.Name] {
						if m.recv == f.recvType && m.dir == f.dir {
							callees[m] = true
							found = true
						}
					}
					if found {
						return false
					}
				}
			}
			for _, m := range p.methods[x.Sel.Name] {
				callees[m] = true
			}
			ast.Inspect(x.X, visit)
			return false
		case *ast.Ident:
			if g := p.plain[f.dir][x.Name]; g != nil {
				callees[g] = true
			}
		}
		return true
	}
	if body != nil {
		ast.Inspect(body, visit)
	}
	delete(callees, f)
	f.calls = sortedFuncs(callees)
	if f.calls == nil {
		f.calls = []*function{}
	}

	used := map[string]bool{}
	var useVisit func(n ast.Node) bool
	useVisit = func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			ast.Inspect(x.X, useVisit)
			return false
		case *ast.Field:
			if x.Type != nil {
				ast.Inspect(x.Type, useVisit)
			}
			return false
		case *ast.Ident:
			if d := p.decls[f.dir][x.Name]; d != nil {
				used[d.key] = true
			}
		}
		return true
	}
	switch n := f.node.(type) {
	case *ast.FuncDecl:
		ast.Inspect(n.Type, useVisit)
		if body != nil {
			ast.Inspect(body, useVisit)
		}
	default:
		ast.Inspect(f.node, useVisit)
	}
	f.uses = sortedKeys(used)
	f.recvUses = []string{}
	if f.recvType != "" {
		if d := p.decls[f.dir][f.recvType]; d != nil {
			f.recvUses = []string{d.key}
		}
	}
}

func sortedFuncs(m map[*function]bool) []*function {
	out := make([]*function, 0, len(m))
	for f := range m {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// declaredFuncs lists the function and method names declared in one file.
func declaredFuncs(name string, src []byte) []string {
	f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.SkipObjectResolution)
	if f == nil || (err != nil && len(f.Decls) == 0) {
		return nil
	}
	var out []string
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			n := fd.Name.Name
			if fd.Recv != nil && len(fd.Recv.List) == 1 {
				n = "(" + recvTypeName(fd.Recv.List[0].Type) + ")." + n
			}
			out = append(out, n)
		}
	}
	return out
}
