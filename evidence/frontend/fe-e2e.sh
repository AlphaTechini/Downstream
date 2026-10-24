#!/bin/sh
# End-to-end check of `downstream frontend` on a tiny static app with planted regressions.
# Usage: fe-e2e.sh <dir holding the downstream binary and a fecheck copy>
set -eu
W=$1
R=$W/repo
rm -rf "$R" && mkdir -p "$R/src"
cd "$R"
git init -q && git config user.email fixture@localhost && git config user.name t

cat > package.json <<'EOF'
{ "name": "fx", "private": true, "scripts": { "build": "node build.mjs" } }
EOF
cat > package-lock.json <<'EOF'
{ "name": "fx", "lockfileVersion": 3, "requires": true, "packages": { "": { "name": "fx" } } }
EOF
cat > build.mjs <<'EOF'
import { cpSync, rmSync } from 'node:fs'
rmSync('dist', { recursive: true, force: true })
cpSync('src', 'dist', { recursive: true })
EOF
cat > downstream.yml <<'EOF'
frontend_build: npm run build
frontend_out: dist
EOF
cat > src/index.html <<'EOF'
<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Shop</title><script type="module" src="/app-AAAAAAAA.js"></script></head>
<body><main><h1>Shop</h1><img src="/logo.png" alt="Shop logo"><ul id="list"></ul></main></body>
</html>
EOF
cat > src/app-AAAAAAAA.js <<'EOF'
const list = document.getElementById('list')
for (let i = 0; i < 5; i++) { const li = document.createElement('li'); li.textContent = 'item ' + i; list.appendChild(li) }
EOF
printf 'PNG' > src/logo.png
git add -A && git commit -qm base
BASE=$(git rev-parse HEAD)

# Head: the bundle grows by a large incompressible blob, renders 300 rows, the logo loses its
# alt text and a banner image that is not in the build is referenced.
git rm -q src/app-AAAAAAAA.js
{
  printf 'const blob = "%s"\n' "$(head -c 40000 /dev/urandom | base64 -w0)"
  echo "const list = document.getElementById('list')"
  echo "for (let i = 0; i < 300; i++) { const li = document.createElement('li'); li.textContent = 'item ' + i + blob.length; list.appendChild(li) }"
} > src/app-BBBBBBBB.js
sed -i 's|app-AAAAAAAA.js|app-BBBBBBBB.js|; s|<img src="/logo.png" alt="Shop logo">|<img src="/logo.png"><img src="/banner.png" alt="Sale">|' src/index.html
git add -A && git commit -qm head
HEAD=$(git rev-parse HEAD)

cat > "$W/fe-journeys.json" <<'EOF'
[{"id":"home","steps":[{"visit":"/"},{"expectText":["h1","Shop"]},{"expectCount":["#list li",5]}]},
 {"id":"bad","steps":[{"visit":"http://example.com/"},{"click":"a[["}]}]
EOF

"$W/downstream" frontend --repo "$R" --base "$BASE" --head "$HEAD" --journeys "$W/fe-journeys.json" --fecheck "$W/fecheck" --out "$W/frontend.json"
"$W/downstream" report --frontend "$W/frontend.json" --out "$W/report.md"
"$W/downstream" issues --frontend "$W/frontend.json" --mr 1 --starter dev --out "$W/issues.json"
cat "$W/report.md"
python3 -c "import json; d=json.load(open('$W/issues.json')); [print('ISSUE:', i['title'], i['steps']) for i in d['issues']]"

# No frontend file changed: head == a commit that only edits the README.
echo readme > README.md && git add -A && git commit -qm docs
"$W/downstream" frontend --repo "$R" --base "$HEAD" --head "$(git rev-parse HEAD)" --journeys "$W/fe-journeys.json" --fecheck "$W/fecheck" --out "$W/skip.json"
