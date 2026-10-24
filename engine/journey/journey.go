// Package journey parses and validates journeys.json, the file the model writes
// and the engine decides whether to trust.
package journey

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"downstream/engine/model"
)

// Limits shared by Validate and Schema so the two cannot drift apart.
const (
	maxJourneys = 20
	maxSteps    = 12
	maxBody     = 4096
	maxPath     = 300
	maxHeader   = 500
	maxText     = 200
	maxExtracts = 8
)

var (
	idRe       = regexp.MustCompile(`^[a-z0-9_]{1,40}$`)
	varRe      = regexp.MustCompile(`^[a-z][a-z0-9_]{0,30}$`)
	placeRe    = regexp.MustCompile(`\{\{([^{}]*)\}\}`)
	headerRe   = regexp.MustCompile(`^(?i:authorization|content-type|accept|x-[a-z0-9-]+)$`)
	methodList = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}
)

// Parse reads journeys.json strictly: unknown fields and trailing data are errors.
func Parse(data []byte) (model.JourneyFile, error) {
	var f model.JourneyFile
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return model.JourneyFile{}, fmt.Errorf("journeys.json: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return model.JourneyFile{}, errors.New("journeys.json: unexpected data after the top-level object")
	}
	return f, nil
}

// Validate checks structure, limits, placeholders and that every step hits a
// route in the head route table. All problems are returned together.
func Validate(f model.JourneyFile, routes []model.Route) error {
	var errs []error
	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if f.Version != model.FormatVersion {
		add("version must be %d, got %d", model.FormatVersion, f.Version)
	}
	if n := len(f.Journeys); n < 1 || n > maxJourneys {
		add("there must be 1 to %d journeys, got %d", maxJourneys, n)
	}

	seenJ := map[string]bool{}
	for ji, j := range f.Journeys {
		jl := fmt.Sprintf("journey %q", j.ID)
		if j.ID == "" {
			jl = fmt.Sprintf("journey #%d", ji+1)
		}
		if !idRe.MatchString(j.ID) {
			add("%s: id must match [a-z0-9_]{1,40}", jl)
		} else if seenJ[j.ID] {
			add("%s: id is used by more than one journey", jl)
		}
		seenJ[j.ID] = true
		switch j.Kind {
		case "", model.KindExisting, model.KindNew, model.KindControl:
		default:
			add("%s: kind must be %q, %q or %q, got %q", jl, model.KindExisting, model.KindNew, model.KindControl, j.Kind)
		}
		checkText(add, jl, "goal", j.Goal)
		checkText(add, jl, "why_included", j.WhyIncluded)
		if n := len(j.Steps); n < 1 || n > maxSteps {
			add("%s: there must be 1 to %d steps, got %d", jl, maxSteps, n)
		}

		seenS := map[string]bool{}
		vars := map[string]bool{"uniq": true}
		for si, s := range j.Steps {
			sl := fmt.Sprintf("%s step %q", jl, s.Name)
			if s.Name == "" {
				sl = fmt.Sprintf("%s step #%d", jl, si+1)
			}
			if !idRe.MatchString(s.Name) {
				add("%s: name must match [a-z0-9_]{1,40}", sl)
			} else if seenS[s.Name] {
				add("%s: name is used by more than one step in this journey", sl)
			}
			seenS[s.Name] = true
			validateStep(add, sl, s, vars, routes)
		}
	}
	return errors.Join(errs...)
}

func checkText(add func(string, ...any), where, field, v string) {
	n := utf8.RuneCountInString(v)
	if n < 1 || n > maxText || strings.TrimSpace(v) == "" {
		add("%s: %s must be 1 to %d characters of text", where, field, maxText)
	}
}

func validateStep(add func(string, ...any), sl string, s model.Step, vars map[string]bool, routes []model.Route) {
	okMethod := false
	for _, m := range methodList {
		if s.Method == m {
			okMethod = true
		}
	}
	if !okMethod {
		add("%s: method must be one of %s, got %q", sl, strings.Join(methodList, ", "), s.Method)
	}

	pathOK := checkPath(add, sl, s.Path)
	checkPlaceholders(add, sl, "path", s.Path, vars)

	for name, val := range s.Headers {
		switch {
		case strings.EqualFold(name, "host"):
			add("%s: the Host header is not allowed", sl)
		case !headerRe.MatchString(name):
			add("%s: header %q is not allowed (only Authorization, Content-Type, Accept and X-*)", sl, name)
		}
		if len(val) > maxHeader || strings.ContainsAny(val, "\r\n\x00") {
			add("%s: header %q has an invalid value", sl, name)
		}
		checkPlaceholders(add, sl, "header "+name, val, vars)
	}
	lower := map[string]string{}
	for name := range s.Headers {
		l := strings.ToLower(name)
		if prev, dup := lower[l]; dup {
			add("%s: headers %q and %q are the same header", sl, prev, name)
		}
		lower[l] = name
	}

	if len(s.Body) > 0 {
		validateBody(add, sl, s.Body, vars)
	}

	if s.ExpectStatus < 100 || s.ExpectStatus > 599 {
		add("%s: expect_status must be 100 to 599, got %d", sl, s.ExpectStatus)
	}

	if pathOK && okMethod && len(routes) > 0 {
		if _, ok := MatchRoute(s.Method, s.Path, routes); !ok {
			add("%s: %s %s matches no route in the head route table", sl, s.Method, s.Path)
		}
	} else if pathOK && okMethod && len(routes) == 0 {
		add("%s: the head route table is empty, so no path can be checked", sl)
	}

	if len(s.Extract) > maxExtracts {
		add("%s: at most %d extracts per step", sl, maxExtracts)
	}
	// Extracts become visible to later steps only, so they are added after the
	// placeholders of this step were checked.
	for name, ptr := range s.Extract {
		switch {
		case !varRe.MatchString(name):
			add("%s: extract name %q must match [a-z][a-z0-9_]{0,30}", sl, name)
		case name == "uniq" || vars[name]:
			add("%s: extract name %q is already defined", sl, name)
		}
		if err := checkPointer(ptr); err != nil {
			add("%s: extract %q: %v", sl, name, err)
		}
	}
	for name := range s.Extract {
		vars[name] = true
	}
}

func checkPath(add func(string, ...any), sl, p string) bool {
	switch {
	case !strings.HasPrefix(p, "/"):
		add("%s: path must start with /", sl)
	case strings.Contains(p, "://"):
		add("%s: path must not contain ://", sl)
	case strings.Contains(p, ".."):
		add("%s: path must not contain ..", sl)
	case len(p) > maxPath:
		add("%s: path is longer than %d bytes", sl, maxPath)
	case strings.ContainsAny(p, " \t\r\n\x00#\\") || strings.HasPrefix(p, "//"):
		add("%s: path contains a character that is not allowed", sl)
	default:
		return true
	}
	return false
}

// checkPlaceholders reports unknown or malformed {{...}} in one string.
func checkPlaceholders(add func(string, ...any), sl, where, s string, vars map[string]bool) {
	for _, m := range placeRe.FindAllStringSubmatch(s, -1) {
		if !varRe.MatchString(m[1]) {
			add("%s: %s has an invalid placeholder {{%s}}", sl, where, m[1])
		} else if !vars[m[1]] {
			add("%s: %s uses {{%s}}, which is neither uniq nor extracted by an earlier step", sl, where, m[1])
		}
	}
	if rest := placeRe.ReplaceAllString(s, ""); strings.Contains(rest, "{{") || strings.Contains(rest, "}}") {
		add("%s: %s has an unbalanced {{ or }}", sl, where)
	}
}

func validateBody(add func(string, ...any), sl string, raw json.RawMessage, vars map[string]bool) {
	if len(raw) > maxBody {
		add("%s: body is %d bytes, the limit is %d", sl, len(raw), maxBody)
		return
	}
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || (t[0] != '{' && t[0] != '[') {
		add("%s: body must be a JSON object or array", sl)
		return
	}
	var v any
	if err := json.Unmarshal(t, &v); err != nil {
		add("%s: body is not valid JSON: %v", sl, err)
		return
	}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			checkPlaceholders(add, sl, "body", x, vars)
		case []any:
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			for k, e := range x {
				if strings.Contains(k, "{{") || strings.Contains(k, "}}") {
					add("%s: placeholders are not allowed in body keys (%q)", sl, k)
				}
				walk(e)
			}
		}
	}
	walk(v)
}

// checkPointer enforces RFC 6901 syntax for a non-empty pointer.
func checkPointer(p string) error {
	if !strings.HasPrefix(p, "/") {
		return fmt.Errorf("JSON pointer %q must start with /", p)
	}
	for i := 0; i < len(p); i++ {
		if p[i] == '~' && (i+1 >= len(p) || (p[i+1] != '0' && p[i+1] != '1')) {
			return fmt.Errorf("JSON pointer %q has ~ not followed by 0 or 1", p)
		}
	}
	if len(p) > 200 {
		return fmt.Errorf("JSON pointer is longer than 200 bytes")
	}
	return nil
}

// MatchRoute finds the route a request would reach. The query string is
// ignored and a segment holding a {{placeholder}} counts as some value, so it
// matches {x} and {x...} but never a literal segment.
func MatchRoute(method, path string, routes []model.Route) (model.Route, bool) {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	if !strings.HasPrefix(path, "/") {
		return model.Route{}, false
	}
	req := strings.Split(path[1:], "/")
	for _, r := range routes {
		if !methodMatches(r.Method, method) || !strings.HasPrefix(r.Path, "/") {
			continue
		}
		if segmentsMatch(strings.Split(r.Path[1:], "/"), req) {
			return r, true
		}
	}
	return model.Route{}, false
}

func methodMatches(routeMethod, method string) bool {
	return routeMethod == "" || routeMethod == method || (routeMethod == "GET" && method == "HEAD")
}

func isWild(seg string) (name string, rest, ok bool) {
	if len(seg) < 3 || seg[0] != '{' || seg[len(seg)-1] != '}' {
		return "", false, false
	}
	name = seg[1 : len(seg)-1]
	if strings.HasSuffix(name, "...") {
		return strings.TrimSuffix(name, "..."), true, true
	}
	return name, false, true
}

func segmentsMatch(pat, req []string) bool {
	for i, p := range pat {
		last := i == len(pat)-1
		if p == "{$}" {
			return last && len(req) == i+1 && req[i] == ""
		}
		if _, rest, ok := isWild(p); ok && rest {
			return last && len(req) >= i+1
		}
		if last && p == "" {
			// A pattern ending in "/" matches the whole subtree below it.
			return len(req) >= i+1
		}
		if i >= len(req) {
			return false
		}
		if _, _, ok := isWild(p); ok {
			if req[i] == "" {
				return false
			}
			continue
		}
		if p != req[i] || strings.Contains(req[i], "{{") {
			return false
		}
	}
	return len(req) == len(pat)
}
