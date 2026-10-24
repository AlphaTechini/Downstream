#!/bin/sh
# Server mode: NewGit renders its own pages. Plant a template change on a scratch clone.
set -eu
W=/tmp/fe-w
if [ ! -x /tmp/go/bin/go ]; then
  curl -fsSL https://go.dev/dl/go1.22.12.linux-amd64.tar.gz | tar -xz -C /tmp
fi
export PATH=/tmp/go/bin:$PATH GOTOOLCHAIN=local GOFLAGS=-modcacherw GOCACHE=/tmp/gocache GOMODCACHE=/tmp/gomodcache
R=$W/newgit
rm -rf "$R"
git clone -q -b main /tmp/fe-w/newgit.bundle "$R"
cd "$R"
git config user.email fixture@localhost && git config user.name t
BASE=$(git rev-parse HEAD)
sed -i 's|<header class="topbar">|<header class="topbar"><img src="/static/promo-banner.png">|' internal/web/templates/layout.html
git commit -qam "Add a promo banner"
HEAD=$(git rev-parse HEAD)
cat > "$W/ng-journeys.json" <<'EOF'
[{"id":"home","steps":[{"visit":"/"}]},
 {"id":"repo","steps":[{"visit":"/demo/storefront"},{"expectText":["h1","storefront"]}]},
 {"id":"issues","steps":[{"visit":"/demo/storefront/-/issues"},{"click":"a[href$='/-/issues/1']"}]}]
EOF
"$W/downstream" frontend --repo "$R" --base "$BASE" --head "$HEAD" --journeys "$W/ng-journeys.json" --fecheck "$W/fecheck" --out "$W/ng-frontend.json"
"$W/downstream" report --frontend "$W/ng-frontend.json" --out "$W/ng-report.md"
"$W/downstream" issues --frontend "$W/ng-frontend.json" --mr 3 --starter dev --out "$W/ng-issues.json"
cat "$W/ng-report.md"
python3 -c "import json; d=json.load(open('$W/ng-issues.json')); [print('ISSUE:', i['title'], i['steps']) for i in d['issues']]"
