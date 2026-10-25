package memory

import (
	"fmt"
	"html/template"
	"net/http"

	"downstream/engine/history"
)

var page = template.Must(template.New("page").Funcs(template.FuncMap{
	"pct": func(v float64) string { return fmt.Sprintf("%+.1f%%", v*100) },
	// metric returns nil for a metric the run did not measure, so the cell shows a dash.
	"metric": func(ms map[string]history.Metric, k string) *history.Metric {
		if m, ok := ms[k]; ok {
			return &m
		}
		return nil
	},
	"short": func(s string) string {
		if len(s) > 8 {
			return s[:8]
		}
		return s
	},
	"day": func(s string) string {
		if len(s) >= 16 {
			return s[:10] + " " + s[11:16] + " UTC"
		}
		return s
	},
}).Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Downstream memory</title>
<style>
:root { --bg:#fbfaf7; --fg:#1d1f21; --muted:#5b6066; --line:#d9d6cf; --up:#a1321f; --down:#1f6b3a; }
@media (prefers-color-scheme: dark) { :root { --bg:#16181b; --fg:#e8e6e1; --muted:#a3a7ad; --line:#33373c; --up:#f08a76; --down:#7fd39b; } }
body { background:var(--bg); color:var(--fg); font:15px/1.5 system-ui, sans-serif; margin:0 auto; max-width:960px; padding:24px 16px; }
h1 { font-size:22px; margin:0 0 4px; } h2 { font-size:17px; margin:28px 0 8px; }
p { color:var(--muted); margin:6px 0; } a { color:inherit; }
table { border-collapse:collapse; width:100%; font-size:14px; }
th, td { border-bottom:1px solid var(--line); padding:6px 8px; text-align:left; white-space:nowrap; }
th { color:var(--muted); font-weight:600; } td.num { text-align:right; font-variant-numeric:tabular-nums; }
.up { color:var(--up); } .down { color:var(--down); }
.wrap { overflow-x:auto; }
</style>
</head>
<body>
<h1>Downstream memory</h1>
<p>Each row is one Downstream run on a merge request: the change in each gated process metric between the target branch and the merge request, measured in the same sandbox session. The flow compounds the changes of merge requests that were merged, so a slow creep shows up even when every merge request passed on its own.</p>
<p>Only opted-in projects are recorded. A record holds the project path, merge request number, commit SHAs and these numbers: no code, no diffs. Records are deleted after {{.Days}} days.</p>
{{range .Projects}}
<h2><a href="https://gitlab.com/{{.Path}}">{{.Path}}</a></h2>
{{if .Runs}}<div class="wrap"><table>
<tr><th>Recorded</th><th>Merge request</th><th>Head</th><th>CPU per journey</th><th>Bytes written per journey</th><th>Peak RSS</th><th>Metrics</th></tr>
{{range .Runs}}<tr>
<td>{{day .Recorded}}</td>
<td><a href="https://gitlab.com/{{.Project}}/-/merge_requests/{{.MR}}">!{{.MR}}</a></td>
<td><code>{{short .Head}}</code></td>
{{with metric .Metrics "app.cpu_ms_per_journey"}}<td class="num {{if gt .Change 0.0}}up{{else}}down{{end}}">{{pct .Change}}</td>{{else}}<td class="num">-</td>{{end}}
{{with metric .Metrics "app.write_bytes_per_journey"}}<td class="num {{if gt .Change 0.0}}up{{else}}down{{end}}">{{pct .Change}}</td>{{else}}<td class="num">-</td>{{end}}
{{with metric .Metrics "app.peak_rss_mb"}}<td class="num {{if gt .Change 0.0}}up{{else}}down{{end}}">{{pct .Change}}</td>{{else}}<td class="num">-</td>{{end}}
<td class="num">{{len .Metrics}}</td>
</tr>{{end}}
</table></div>{{else}}<p>No runs recorded in the last {{$.Days}} days.</p>{{end}}
{{end}}
<p>Source: <a href="https://gitlab.com/gitlab-ai-hackathon/transcend-october-2026/35141659/showcase">Downstream on GitLab</a>. API: <code>GET /v1/runs?project=&lt;path&gt;</code>.</p>
</body>
</html>
`))

type dashProject struct {
	Path string
	Runs any
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	var ps []dashProject
	for _, p := range s.Allowed {
		es, err := s.Store.List(r.Context(), p)
		if err != nil {
			s.Log.Warn("dashboard: reading history failed", "project", p, "error", err.Error())
		}
		if len(es) > 30 {
			es = es[:30]
		}
		ps = append(ps, dashProject{Path: p, Runs: es})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = page.Execute(w, map[string]any{"Projects": ps, "Days": int(s.Retention.Hours() / 24)})
}
