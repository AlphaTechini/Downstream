package scan

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"downstream/engine/model"
)

// maxHunkText caps the hunk body stored in scan.json.
const maxHunkText = 2048

const truncMark = "\n[truncated]"

// Status values of a file in a diff.
const (
	statusModified = "modified"
	statusAdded    = "added"
	statusDeleted  = "deleted"
	statusRenamed  = "renamed"
)

// fileDiff is one file of a unified diff.
type fileDiff struct {
	model.ChangedFile
	Status  string
	OldPath string // path on the old side; empty for added files
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// parseDiff reads the output of git diff (any context size). Hunk bodies are
// consumed by the line counts in their headers, so a removed line that looks
// like a file header ("--- x") cannot be mistaken for one.
func parseDiff(text string) ([]fileDiff, error) {
	var files []fileDiff
	cur := -1
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		switch {
		case strings.HasPrefix(line, "diff --git "):
			a, b := splitGitHeader(strings.TrimPrefix(line, "diff --git "))
			files = append(files, fileDiff{
				ChangedFile: model.ChangedFile{Path: b, Hunks: []model.Hunk{}},
				Status:      statusModified,
				OldPath:     a,
			})
			cur = len(files) - 1
		case cur < 0:
			continue
		case strings.HasPrefix(line, "new file mode"):
			files[cur].Status = statusAdded
			files[cur].OldPath = ""
		case strings.HasPrefix(line, "deleted file mode"):
			files[cur].Status = statusDeleted
		case strings.HasPrefix(line, "rename from "):
			files[cur].Status = statusRenamed
			files[cur].OldPath = unquoteGit(strings.TrimPrefix(line, "rename from "))
		case strings.HasPrefix(line, "rename to "):
			files[cur].Path = unquoteGit(strings.TrimPrefix(line, "rename to "))
		case strings.HasPrefix(line, "--- "):
			p := unquoteGit(line[4:])
			if p == "/dev/null" {
				files[cur].OldPath = ""
			} else {
				files[cur].OldPath = stripSide(p, "a/")
			}
		case strings.HasPrefix(line, "+++ "):
			p := unquoteGit(line[4:])
			if p == "/dev/null" {
				files[cur].Status = statusDeleted
				if files[cur].OldPath != "" {
					files[cur].Path = files[cur].OldPath
				}
			} else {
				files[cur].Path = stripSide(p, "b/")
			}
		case strings.HasPrefix(line, "@@ "):
			h, err := parseHunkHeader(line)
			if err != nil {
				return nil, err
			}
			oldRem, newRem := h.OldLines, h.NewLines
			var body []string
			for oldRem > 0 || newRem > 0 {
				i++
				if i >= len(lines) {
					return nil, fmt.Errorf("diff ends inside the hunk %q", line)
				}
				l := lines[i]
				switch {
				case strings.HasPrefix(l, "+"):
					newRem--
				case strings.HasPrefix(l, "-"):
					oldRem--
				case strings.HasPrefix(l, " "), l == "":
					oldRem--
					newRem--
				case strings.HasPrefix(l, `\`):
					continue
				default:
					return nil, fmt.Errorf("unexpected line in hunk %q: %q", line, l)
				}
				body = append(body, l)
			}
			h.Text = capText(strings.Join(body, "\n"))
			files[cur].Hunks = append(files[cur].Hunks, h)
		}
	}
	return files, nil
}

func parseHunkHeader(line string) (model.Hunk, error) {
	m := hunkHeader.FindStringSubmatch(line)
	if m == nil {
		return model.Hunk{}, fmt.Errorf("bad hunk header %q", line)
	}
	num := func(s string, omitted int) int {
		if s == "" {
			return omitted
		}
		n, _ := strconv.Atoi(s)
		return n
	}
	return model.Hunk{
		OldStart: num(m[1], 0),
		OldLines: num(m[2], 1),
		NewStart: num(m[3], 0),
		NewLines: num(m[4], 1),
	}, nil
}

func capText(s string) string {
	if len(s) <= maxHunkText {
		return s
	}
	n := maxHunkText - len(truncMark)
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + truncMark
}

// splitGitHeader splits the "a/X b/Y" part of a "diff --git" line. Renames
// are corrected later from the "rename from/to" lines.
func splitGitHeader(s string) (a, b string) {
	if strings.HasPrefix(s, `"`) {
		if end := closingQuote(s); end > 0 {
			a = stripSide(unquoteGit(s[:end+1]), "a/")
			b = stripSide(unquoteGit(strings.TrimSpace(s[end+1:])), "b/")
			return a, b
		}
	}
	if n := (len(s) - 5) / 2; len(s) >= 5 && len(s)%2 == 1 &&
		strings.HasPrefix(s, "a/") && s[2+n:n+5] == " b/" && s[2:2+n] == s[n+5:] {
		return s[2 : 2+n], s[n+5:]
	}
	if idx := strings.LastIndex(s, ` "b/`); idx > 0 {
		return stripSide(s[:idx], "a/"), stripSide(unquoteGit(s[idx+1:]), "b/")
	}
	if idx := strings.LastIndex(s, " b/"); idx > 0 {
		return stripSide(s[:idx], "a/"), s[idx+3:]
	}
	return "", ""
}

func closingQuote(s string) int {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i
		}
	}
	return -1
}

// unquoteGit undoes git's C-style path quoting and the trailing tab git adds
// after names that contain spaces.
func unquoteGit(s string) string {
	s = strings.TrimRight(s, "\t")
	if strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
	}
	return s
}

func stripSide(p, prefix string) string {
	return strings.TrimPrefix(p, prefix)
}
