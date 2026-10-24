package runner

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// registry is where an exact Node version is fetched from: the npm package
// node-linux-x64 holds the official binary at package/bin/node.
const registry = "https://registry.npmjs.org/node-linux-x64/"

const maxNodeTarball = 200 << 20

// nodeVersion returns the version of the node on PATH, without the "v".
func nodeVersion(ctx context.Context, env []string) (string, error) {
	cmd := exec.CommandContext(ctx, "node", "--version")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("node was not found on PATH: %w", err)
	}
	return strings.TrimPrefix(strings.TrimSpace(string(out)), "v"), nil
}

// fetchNode downloads node-linux-x64@version, checks it against the registry's
// sha512 integrity value, and unpacks it under dir. It returns the directory
// holding the node binary.
func fetchNode(ctx context.Context, version, dir string) (string, error) {
	var meta struct {
		Dist struct {
			Tarball   string `json:"tarball"`
			Integrity string `json:"integrity"`
		} `json:"dist"`
	}
	body, err := httpGet(ctx, registry+version, 1<<20)
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		return "", fmt.Errorf("registry metadata for node %s: %w", version, err)
	}
	want, ok := strings.CutPrefix(meta.Dist.Integrity, "sha512-")
	if !ok || !strings.HasPrefix(meta.Dist.Tarball, "https://registry.npmjs.org/") {
		return "", fmt.Errorf("registry metadata for node %s has no sha512 integrity or an unexpected tarball URL", version)
	}
	tgz, err := httpGet(ctx, meta.Dist.Tarball, maxNodeTarball)
	if err != nil {
		return "", err
	}
	sum := sha512.Sum512(tgz)
	if base64.StdEncoding.EncodeToString(sum[:]) != want {
		return "", fmt.Errorf("node %s tarball does not match the registry's sha512", version)
	}
	zr, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		return "", err
	}
	if err := extractTar(zr, dir); err != nil {
		return "", fmt.Errorf("unpacking node %s: %w", version, err)
	}
	bin := filepath.Join(dir, "package", "bin")
	if st, err := os.Stat(filepath.Join(bin, "node")); err != nil || !st.Mode().IsRegular() {
		return "", fmt.Errorf("node %s package has no bin/node", version)
	}
	return bin, nil
}

func httpGet(ctx context.Context, url string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("GET %s: response larger than %d bytes", url, limit)
	}
	return b, nil
}

// withPath puts dir first on PATH in env.
func withPath(env []string, dir string) []string {
	out := make([]string, 0, len(env)+1)
	set := false
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			kv = "PATH=" + dir + string(os.PathListSeparator) + v
			set = true
		}
		out = append(out, kv)
	}
	if !set {
		out = append(out, "PATH="+dir)
	}
	return out
}

// shell runs command with sh in dir under the build timeout, killing the
// whole process group on timeout. The error carries the output tail.
func shell(ctx context.Context, dir, command string, env []string) error {
	ctx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	out := &tailBuffer{max: 4096}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%s stopped (%w); output tail:\n%s", command, ctx.Err(), out.String())
		}
		return fmt.Errorf("%s failed (%w); output tail:\n%s", command, err, out.String())
	}
	return nil
}

// installDeps runs npm ci in dir. Install scripts are skipped: current
// bundlers ship native parts as optional platform packages and do not need
// them. Only npm lockfiles are supported.
func installDeps(ctx context.Context, dir string, env []string) error {
	if _, err := os.Stat(filepath.Join(dir, "package-lock.json")); err != nil {
		for _, other := range []string{"pnpm-lock.yaml", "yarn.lock", "bun.lockb"} {
			if _, err := os.Stat(filepath.Join(dir, other)); err == nil {
				return fmt.Errorf("found %s; only package-lock.json (npm ci) is supported", other)
			}
		}
		return errors.New("no package-lock.json in frontend_dir; npm ci needs one")
	}
	return shell(ctx, dir, "npm ci --ignore-scripts --no-audit --no-fund", env)
}

// sameFile reports whether two files exist and hold the same bytes.
func sameFile(a, b string) bool {
	x, err := os.ReadFile(a)
	if err != nil {
		return false
	}
	y, err := os.ReadFile(b)
	return err == nil && bytes.Equal(x, y)
}

// linkTree recreates src at dst with hard links, so the head build reuses the
// base install when both commits have the same lockfile. Symlinks are copied as
// symlinks.
func linkTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		default:
			return os.Link(p, target)
		}
	})
}
