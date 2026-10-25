// Command downstream-memory serves the Downstream memory API and dashboard on
// Cloud Run. Configuration comes from the environment:
//
//	PORT               listen port (Cloud Run sets it)
//	ALLOWED_PROJECTS   comma-separated GitLab project paths that may record runs
//	RETENTION_DAYS     days a record is kept (default 30)
//	GITLAB_API         GitLab API base (default https://gitlab.com/api/v4)
//	STORE              "firestore" (default) or "memory" for local runs
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"downstream/engine/memory"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{ReplaceAttr: cloudLogging}))
	ctx := context.Background()

	var store memory.Store
	if os.Getenv("STORE") == "memory" {
		store = memory.NewMemStore()
	} else {
		fs, err := memory.NewFirestore(ctx)
		if err != nil {
			log.Error("starting the Firestore store", "error", err.Error())
			os.Exit(1)
		}
		store = fs
	}
	days := 30
	if v, err := strconv.Atoi(os.Getenv("RETENTION_DAYS")); err == nil && v > 0 {
		days = v
	}
	api := os.Getenv("GITLAB_API")
	if api == "" {
		api = "https://gitlab.com/api/v4"
	}
	var allowed []string
	for _, p := range strings.Split(os.Getenv("ALLOWED_PROJECTS"), ",") {
		if p = strings.TrimSpace(p); p != "" {
			allowed = append(allowed, p)
		}
	}
	s := &memory.Server{
		Store:        store,
		Allowed:      allowed,
		Retention:    time.Duration(days) * 24 * time.Hour,
		CommitExists: memory.GitLabCommitExists(api, &http.Client{Timeout: 10 * time.Second}),
		Log:          log,
		Now:          time.Now,
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Info("downstream-memory listening", "port", port, "allowed_projects", len(allowed), "retention_days", days)
	srv := &http.Server{Addr: ":" + port, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		log.Error("server stopped", "error", err.Error())
		os.Exit(1)
	}
}

// cloudLogging renames slog's keys to the ones Cloud Logging reads from JSON
// lines, so each line shows its severity and message in the console.
func cloudLogging(groups []string, a slog.Attr) slog.Attr {
	if len(groups) > 0 {
		return a
	}
	switch a.Key {
	case slog.LevelKey:
		a.Key = "severity"
	case slog.MessageKey:
		a.Key = "message"
	case slog.TimeKey:
		a.Key = "time"
	}
	return a
}
