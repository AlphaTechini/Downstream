package scan

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const maxSourceFile = 4 << 20

var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// git runs git in repo with an argument list; no shell is involved.
func git(repo string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func resolveCommit(repo, rev string) (string, error) {
	if rev == "" || strings.HasPrefix(rev, "-") {
		return "", fmt.Errorf("invalid revision %q", rev)
	}
	out, err := git(repo, "rev-parse", "--verify", rev+"^{commit}")
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(string(out))
	if !fullSHA.MatchString(sha) {
		return "", fmt.Errorf("git returned %q for %q, not a commit id", sha, rev)
	}
	return sha, nil
}

// cleanRoot normalises the scan root: slash separated, relative, inside the repo.
func cleanRoot(root string) (string, error) {
	r := path.Clean(filepath.ToSlash(strings.TrimSpace(root)))
	if r == "" {
		r = "."
	}
	if path.IsAbs(r) || r == ".." || strings.HasPrefix(r, "../") {
		return "", fmt.Errorf("root %q must be a directory inside the repo", root)
	}
	return r, nil
}

func treeish(sha, root string) string {
	if root == "." {
		return sha
	}
	return sha + ":" + root
}

func existsInCommit(repo, spec string) bool {
	_, err := git(repo, "cat-file", "-e", spec)
	return err == nil
}

// extractGo writes the Go sources of a tree (git archive piped into an
// in-memory tar reader) under dst. The working tree of the repo is not touched.
func extractGo(repo, tree, dst string) error {
	cmd := exec.Command("git", "-C", repo, "archive", "--format=tar", tree)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	readErr := untarGo(stdout, dst)
	_, _ = io.Copy(io.Discard, stdout)
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("git archive %s: %w: %s", tree, err, strings.TrimSpace(stderr.String()))
	}
	return readErr
}

func untarGo(r io.Reader, dst string) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := path.Clean(hdr.Name)
		if path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") || !goSource(path.Base(name)) {
			continue
		}
		skip := false
		for _, part := range strings.Split(path.Dir(name), "/") {
			if part != "." && skipDirName(part) {
				skip = true
			}
		}
		if skip {
			continue
		}
		target := filepath.Join(dst, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		n, err := io.Copy(f, io.LimitReader(tr, maxSourceFile+1))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		if n > maxSourceFile {
			return fmt.Errorf("%s is larger than %d bytes", name, maxSourceFile)
		}
	}
}

// moduleFromGit finds the nearest go.mod at or above root in a commit.
func moduleFromGit(repo, sha, root string) (module, base string) {
	for d := root; ; d = path.Dir(d) {
		spec := sha + ":go.mod"
		if d != "." {
			spec = sha + ":" + d + "/go.mod"
		}
		if out, err := git(repo, "cat-file", "blob", spec); err == nil {
			if m := parseModulePath(string(out)); m != "" {
				switch {
				case d == ".":
					return m, root
				case d == root:
					return m, "."
				default:
					return m, strings.TrimPrefix(root, d+"/")
				}
			}
		}
		if d == "." {
			return "", ""
		}
	}
}
