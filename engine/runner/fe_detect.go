package runner

import (
	"context"
	"fmt"
	"os/exec"
	"path"
	"strings"

	"downstream/engine/target"
)

// frontendExts are the file types that change what a browser receives.
var frontendExts = map[string]bool{
	".html": true, ".htm": true, ".css": true, ".scss": true, ".sass": true, ".less": true,
	".js": true, ".mjs": true, ".cjs": true, ".jsx": true, ".ts": true, ".tsx": true,
	".vue": true, ".svelte": true, ".astro": true,
	".svg": true, ".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".avif": true, ".ico": true,
	".woff": true, ".woff2": true,
	".tmpl": true, ".gohtml": true, // server-rendered Go templates
}

// frontendManifests change the bundle even though they are not source files.
var frontendManifests = map[string]bool{
	"package.json": true, "package-lock.json": true,
	"vite.config.js": true, "vite.config.ts": true, "svelte.config.js": true, "angular.json": true,
}

// changedFiles lists the paths that differ between base and head.
func changedFiles(ctx context.Context, repo, base, head string) ([]string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", repo, "diff", "--name-only", "--no-renames", base, head, "--").Output()
	if err != nil {
		return nil, fmt.Errorf("git diff %s %s: %w", short(base), short(head), err)
	}
	var files []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			files = append(files, l)
		}
	}
	return files, nil
}

// frontendChanges returns the changed files that can alter the pages. With a
// declared frontend build, only files under frontend_dir count; the backend's
// own templates and assets count otherwise.
func frontendChanges(cfg target.Config, files []string) []string {
	var hits []string
	for _, f := range files {
		if cfg.HasStaticFrontend() && cfg.Frontend.Dir != "." && !strings.HasPrefix(f, strings.TrimSuffix(cfg.Frontend.Dir, "/")+"/") {
			continue
		}
		if cfg.HasStaticFrontend() && strings.HasPrefix(f, strings.TrimSuffix(cfg.Frontend.Out, "/")+"/") {
			continue // committed build output is rebuilt anyway
		}
		if frontendExts[strings.ToLower(path.Ext(f))] || frontendManifests[path.Base(f)] {
			hits = append(hits, f)
		}
	}
	return hits
}
