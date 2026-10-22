package scan

import (
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"downstream/engine/model"
)

// mwRef is a function that wraps a handler.
type mwRef struct {
	name  string
	roots []*function
}

// routeInfo is a route plus what the call graph needs to walk from it.
type routeInfo struct {
	model.Route
	handlerRoots []*function
	mws          []mwRef
}

// Routes parses the Go files under dir (recursively, skipping _test.go files
// and testdata) and returns the routes registered with HandleFunc or Handle on
// anything. File names are dir joined with the path inside it.
func Routes(dir string) ([]model.Route, error) {
	cfg := loadConfig{dir: dir, prefix: filepath.ToSlash(filepath.Clean(dir))}
	cfg.module, cfg.modBase = moduleFromDisk(dir)
	p, err := load(cfg)
	if err != nil {
		return nil, err
	}
	out := make([]model.Route, 0, len(p.routes))
	for _, r := range p.routes {
		out = append(out, r.Route)
	}
	return out, nil
}

// moduleFromDisk finds the nearest go.mod at or above dir.
func moduleFromDisk(dir string) (module, base string) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", ""
	}
	for d, up := abs, 0; up < 12; d, up = filepath.Dir(d), up+1 {
		if data, err := os.ReadFile(filepath.Join(d, "go.mod")); err == nil {
			rel, err := filepath.Rel(d, abs)
			if err != nil {
				return "", ""
			}
			return parseModulePath(string(data)), filepath.ToSlash(rel)
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	return "", ""
}

func parseModulePath(gomod string) string {
	for _, line := range strings.Split(gomod, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "module" {
			return strings.Trim(f[1], `"`)
		}
	}
	return ""
}

func (p *program) extractRoutes(fi *fileInfo) {
	for _, d := range fi.file.Decls {
		ctx := &function{dir: fi.dir, file: fi.path, fi: fi}
		if fd, ok := d.(*ast.FuncDecl); ok {
			if f := p.byNode[fd]; f != nil {
				ctx = f
			}
		}
		ast.Inspect(d, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "HandleFunc" && sel.Sel.Name != "Handle") {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok {
				return true
			}
			pattern, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			p.addRoute(ctx, call, pattern)
			return true
		})
	}
}

func (p *program) addRoute(ctx *function, call *ast.CallExpr, pattern string) {
	method, rpath := splitPattern(pattern)
	line := p.line(call.Pos())
	ri := &routeInfo{Route: model.Route{Method: method, Path: rpath, File: ctx.file, Line: line}}
	if !p.analyseHandler(ctx, call.Args[1], ri, true, true) && !p.analyseHandler(ctx, call.Args[1], ri, false, true) {
		ri.Handler = types.ExprString(call.Args[1])
	}
	for _, m := range ri.mws {
		ri.Middleware = append(ri.Middleware, m.name)
	}
	if len(ri.handlerRoots) == 0 {
		p.notes = append(p.notes, fmt.Sprintf("route %s: handler %s is not a function declared in %s, so its call path was not traced", label(ri.Route), ri.Handler, p.cfg.prefix))
	}
	p.routes = append(p.routes, ri)
}

func splitPattern(pattern string) (method, rpath string) {
	f := strings.Fields(pattern)
	switch {
	case len(f) >= 2:
		return f[0], f[1]
	case len(f) == 1:
		s := f[0]
		if i := strings.Index(s, "/"); i > 0 {
			s = s[i:] // drop a host
		}
		return "", s
	}
	return "", pattern
}

func label(r model.Route) string {
	if r.Method == "" {
		return r.Path
	}
	return r.Method + " " + r.Path
}

// analyseHandler walks a handler expression. The innermost function reference
// becomes the handler and each call wrapped around it, outermost first, a
// middleware. In strict mode a bare name only counts if it is a function
// declared in the tree, which keeps arguments like cfg.Timeout from being
// taken for the handler.
func (p *program) analyseHandler(ctx *function, e ast.Expr, ri *routeInfo, strict, top bool) bool {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return p.analyseHandler(ctx, x.X, ri, strict, top)
	case *ast.FuncLit:
		start := p.line(x.Pos())
		lit := &function{
			name:     fmt.Sprintf("func@%s:%d", ctx.file, start),
			dir:      ctx.dir,
			file:     ctx.file,
			start:    start,
			end:      p.line(x.End()),
			fi:       ctx.fi,
			recvName: ctx.recvName,
			recvType: ctx.recvType,
			node:     x,
		}
		lit.key = lit.name
		p.addFunc(lit)
		p.collect(lit)
		ri.Handler = lit.key
		ri.handlerRoots = []*function{lit}
		return true
	case *ast.CallExpr:
		if isHandlerFuncConv(x.Fun) && len(x.Args) == 1 {
			return p.analyseHandler(ctx, x.Args[0], ri, strict, top)
		}
		name, roots, _ := p.resolveRef(ctx, x.Fun)
		ri.mws = append(ri.mws, mwRef{name: name, roots: roots})
		for _, s := range []bool{true, false} {
			for i := len(x.Args) - 1; i >= 0; i-- {
				if p.analyseHandler(ctx, x.Args[i], ri, s, false) {
					return true
				}
			}
		}
		// Nothing inside: the call itself produces the handler, e.g. http.FileServer(dir).
		// Inside another call, a call with only literal arguments (http.Dir(".")) is a
		// value, not a handler, unless it is a function declared in the tree.
		ri.mws = ri.mws[:len(ri.mws)-1]
		if !top && len(roots) == 0 && !hasNonLiteralArg(x) {
			return false
		}
		ri.Handler = name
		ri.handlerRoots = roots
		return true
	case *ast.Ident, *ast.SelectorExpr:
		name, roots, _ := p.resolveRef(ctx, e)
		if strict && len(roots) == 0 {
			return false
		}
		ri.Handler = name
		ri.handlerRoots = roots
		return true
	}
	return false
}

func hasNonLiteralArg(c *ast.CallExpr) bool {
	for _, a := range c.Args {
		if _, ok := a.(*ast.BasicLit); !ok {
			return true
		}
	}
	return false
}

func isHandlerFuncConv(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.SelectorExpr:
		return x.Sel.Name == "HandlerFunc"
	case *ast.Ident:
		return x.Name == "HandlerFunc"
	}
	return false
}

// resolveRef names a function reference and finds the declared functions it
// can stand for. A method reference that several types satisfy keeps its
// source text as the name and returns all candidates.
func (p *program) resolveRef(ctx *function, e ast.Expr) (name string, roots []*function, resolved bool) {
	text := types.ExprString(e)
	switch x := e.(type) {
	case *ast.CallExpr:
		// curried middleware such as requireRole("admin")(h)
		return p.resolveRef(ctx, x.Fun)
	case *ast.Ident:
		if f := p.plain[ctx.dir][x.Name]; f != nil {
			return f.key, []*function{f}, true
		}
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			if dir, isImport := ctx.fi.imports[id.Name]; isImport {
				if dir != "" {
					if f := p.plain[dir][x.Sel.Name]; f != nil {
						return f.key, []*function{f}, true
					}
				}
				return text, nil, false
			}
			if ctx.recvName != "" && id.Name == ctx.recvName {
				for _, m := range p.methods[x.Sel.Name] {
					if m.recv == ctx.recvType && m.dir == ctx.dir {
						return m.key, []*function{m}, true
					}
				}
			}
		}
		switch cands := p.methods[x.Sel.Name]; len(cands) {
		case 0:
		case 1:
			return cands[0].key, cands, true
		default:
			return text, cands, true
		}
	}
	return text, nil, false
}
