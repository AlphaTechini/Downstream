#!/usr/bin/env python3
"""Validate the Duo flow YAML and agent config in this repository.

Checks only what we have observed or what is structurally required for the files to
mean anything. It is not GitLab's validator; GitLab's own checks still apply when a flow
is saved. Exit code 0 means every check passed.

Usage: python3 scripts/validate_yaml.py [repo_root]
"""
import re
import sys
from pathlib import Path

try:
    import yaml
except ImportError:  # pragma: no cover
    print("PyYAML is required: pip install pyyaml", file=sys.stderr)
    sys.exit(2)

FLOW_TOP = {"version", "environment", "components", "routers", "flow", "prompts"}
CONFIG_KEYS = {"image", "setup_script", "network_policy", "cache"}
NETWORK_KEYS = {"allowed_domains", "denied_domains", "include_recommended_allowed",
                "allow_all_unix_sockets"}
VAR = re.compile(r"\{\{(.*?)\}\}")


def load(path):
    with open(path, encoding="utf-8") as fh:
        return yaml.safe_load(fh)


def check_flow(path, errors):
    d = load(path)
    if not isinstance(d, dict):
        errors.append(f"{path}: top level is not a mapping")
        return
    missing = FLOW_TOP - set(d)
    if missing:
        errors.append(f"{path}: missing top-level keys {sorted(missing)}")
        return
    if d["version"] != "v1":
        errors.append(f"{path}: version is {d['version']!r}, expected 'v1'")
    comps = d["components"]
    names = []
    prompt_ids = {p.get("prompt_id") for p in d["prompts"]}
    prompts = {p.get("prompt_id"): p for p in d["prompts"]}
    for c in comps:
        for k in ("name", "type"):
            if k not in c:
                errors.append(f"{path}: component without '{k}': {c}")
        names.append(c.get("name"))
        pid = c.get("prompt_id")
        if pid not in prompt_ids:
            errors.append(f"{path}: component {c.get('name')!r} uses unknown prompt_id {pid!r}")
            continue
        declared = set()
        for i in c.get("inputs", []):
            if isinstance(i, dict):
                declared.add(i.get("as"))
            else:  # bare string input is normalised by the platform
                declared.add(str(i).split(":")[-1])
        tpl = prompts[pid].get("prompt_template", {})
        for part in ("system", "user"):
            text = tpl.get(part, "") or ""
            for m in VAR.finditer(text):
                inner = m.group(1)
                if not (inner.startswith(" ") and inner.endswith(" ")) or not inner.strip():
                    errors.append(f"{path}: '{{{{{inner}}}}}' needs a space inside the braces")
                    continue
                if inner.strip() not in declared:
                    errors.append(f"{path}: prompt {pid!r} uses {{{{ {inner.strip()} }}}} "
                                  f"but component inputs declare {sorted(declared)}")
    if len(set(names)) != len(names):
        errors.append(f"{path}: duplicate component names {names}")
    valid_targets = set(names) | {"end"}
    for r in d["routers"]:
        if r.get("from") not in names:
            errors.append(f"{path}: router from {r.get('from')!r} is not a component")
        if r.get("to") not in valid_targets:
            errors.append(f"{path}: router to {r.get('to')!r} is not a component or 'end'")
    ep = d["flow"].get("entry_point") if isinstance(d["flow"], dict) else None
    if ep not in names:
        errors.append(f"{path}: flow.entry_point {ep!r} is not a component")
    if path.name.endswith(".draft.yml"):
        first = path.read_text(encoding="utf-8").lstrip().splitlines()[0]
        if not first.startswith("# DRAFT"):
            errors.append(f"{path}: draft flows must start with a '# DRAFT' comment")


def check_config(path, errors):
    d = load(path)
    if not isinstance(d, dict):
        errors.append(f"{path}: top level is not a mapping")
        return
    unknown = set(d) - CONFIG_KEYS
    if unknown:
        errors.append(f"{path}: unknown keys {sorted(unknown)}")
    ss = d.get("setup_script")
    if ss is not None:
        ok = isinstance(ss, str) or (isinstance(ss, list) and all(isinstance(x, str) for x in ss))
        if not ok:
            errors.append(f"{path}: setup_script must be a string or a list of strings")
    np = d.get("network_policy")
    if np is not None:
        if not isinstance(np, dict):
            errors.append(f"{path}: network_policy must be a mapping")
        else:
            bad = set(np) - NETWORK_KEYS
            if bad:
                errors.append(f"{path}: unknown network_policy keys {sorted(bad)}")
            for k in ("allowed_domains", "denied_domains"):
                v = np.get(k, [])
                if not (isinstance(v, list) and all(isinstance(x, str) for x in v)):
                    errors.append(f"{path}: {k} must be a list of strings")
            for k in ("include_recommended_allowed", "allow_all_unix_sockets"):
                if k in np and not isinstance(np[k], bool):
                    errors.append(f"{path}: {k} must be true or false")


def main():
    root = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).resolve().parent.parent
    errors = []
    flows = sorted((root / "flows").glob("*.yml"))
    configs = sorted((root / ".gitlab" / "duo").glob("agent-config.yml"))
    if not flows:
        errors.append("no flow files found under flows/")
    for f in flows:
        try:
            check_flow(f, errors)
        except Exception as exc:  # malformed YAML etc.
            errors.append(f"{f}: {type(exc).__name__}: {exc}")
    for c in configs:
        try:
            check_config(c, errors)
        except Exception as exc:
            errors.append(f"{c}: {type(exc).__name__}: {exc}")
    if errors:
        print("FAIL")
        for e in errors:
            print(" -", e)
        return 1
    print(f"ok: {len(flows)} flow file(s), {len(configs)} agent config file(s)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
