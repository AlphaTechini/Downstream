package runner

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// maxTreeBytes bounds what one commit may unpack to, so a hostile archive
// cannot fill the disk of a CI job.
const maxTreeBytes = 1 << 30

// materialise writes the tree of commit into dir without touching the
// repository's working tree or refs.
func materialise(ctx context.Context, repo, commit, dir string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", repo, "archive", "--format=tar", commit)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr := &tailBuffer{max: 4096}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	xerr := extractTar(stdout, dir)
	if xerr != nil {
		_ = cmd.Cancel()
	}
	_, _ = io.Copy(io.Discard, stdout)
	werr := cmd.Wait()
	if xerr != nil {
		return xerr
	}
	if werr != nil {
		return fmt.Errorf("git archive %s: %w: %s", commit, werr, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// extractTar unpacks r under dir. It refuses absolute names and any name that
// climbs out of dir, skips links that point outside, and never writes through
// a symlink it created earlier.
func extractTar(r io.Reader, dir string) error {
	root, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	tr := tar.NewReader(r)
	symlinks := map[string]bool{}
	var total int64
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading archive: %w", err)
		}
		switch hdr.Typeflag {
		case tar.TypeXGlobalHeader, tar.TypeXHeader:
			continue
		}
		name, err := safeName(hdr.Name)
		if err != nil {
			return err
		}
		if name == "." {
			continue
		}
		for p := path.Dir(name); p != "."; p = path.Dir(p) {
			if symlinks[p] {
				return fmt.Errorf("archive entry %q is below a symlink", hdr.Name)
			}
		}
		target := filepath.Join(root, filepath.FromSlash(name))

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			total += hdr.Size
			if total > maxTreeBytes {
				return fmt.Errorf("archive is larger than %d bytes", int64(maxTreeBytes))
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(hdr.Mode).Perm() | 0o600
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			_, cerr := io.Copy(f, tr)
			if err := f.Close(); cerr == nil {
				cerr = err
			}
			if cerr != nil {
				return cerr
			}
		case tar.TypeSymlink:
			if !linkStaysInside(name, hdr.Linkname) {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
			symlinks[name] = true
		default:
			// Hard links, devices and fifos have no place in a source tree.
		}
	}
}

// safeName cleans an archive path and rejects absolute or escaping names.
func safeName(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, 0) || strings.HasPrefix(name, "/") || strings.Contains(name, `\`) {
		return "", fmt.Errorf("archive entry %q has an unsafe path", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", fmt.Errorf("archive entry %q has an unsafe path", name)
		}
	}
	return path.Clean(name), nil
}

// linkStaysInside reports whether a symlink at name with the given target
// resolves to somewhere inside the tree, judged lexically.
func linkStaysInside(name, linkname string) bool {
	if linkname == "" || strings.HasPrefix(linkname, "/") || strings.ContainsRune(linkname, 0) {
		return false
	}
	joined := path.Join(path.Dir(name), linkname)
	return joined != ".." && !strings.HasPrefix(joined, "../")
}
