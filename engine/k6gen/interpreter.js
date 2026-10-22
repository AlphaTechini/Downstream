const OPTIONS_SCENARIOS = {};
const M = {};
for (const j of JOURNEYS) {
  OPTIONS_SCENARIOS[j.id] = {
    executor: 'per-vu-iterations',
    exec: 'j_' + j.id,
    vus: VUS,
    iterations: ITERATIONS,
    maxDuration: MAX_DURATION,
    gracefulStop: '5s',
  };
  for (const s of j.steps) {
    const id = j.id + '__' + s.name;
    M[id] = {
      dur: new Trend('ds_' + id + '_duration', true),
      reqs: new Counter('ds_' + id + '_reqs'),
      fails: new Counter('ds_' + id + '_fails'),
    };
  }
}

export const options = {
  summaryTrendStats: ['count', 'avg', 'med', 'p(95)', 'min', 'max'],
  scenarios: OPTIONS_SCENARIOS,
  // A redirect could lead off loopback, so none is followed.
  maxRedirects: 0,
};

function sub(str, vars, inPath) {
  return str.replace(/\{\{([a-z][a-z0-9_]*)\}\}/g, (m, k) => {
    if (!Object.prototype.hasOwnProperty.call(vars, k)) return m;
    const v = String(vars[k]).replace(/[\r\n]/g, '');
    return inPath ? encodeURIComponent(v) : v;
  });
}

function subDeep(v, vars) {
  if (typeof v === 'string') return sub(v, vars, false);
  if (Array.isArray(v)) return v.map((e) => subDeep(e, vars));
  if (v !== null && typeof v === 'object') {
    const o = {};
    for (const k of Object.keys(v)) o[k] = subDeep(v[k], vars);
    return o;
  }
  return v;
}

// RFC 6901 lookup; undefined means the value is missing.
function resolvePointer(doc, ptr) {
  if (ptr === '') return doc;
  if (ptr[0] !== '/') return undefined;
  let cur = doc;
  for (const raw of ptr.slice(1).split('/')) {
    const key = raw.replace(/~1/g, '/').replace(/~0/g, '~');
    if (Array.isArray(cur)) {
      if (!/^(0|[1-9][0-9]*)$/.test(key)) return undefined;
      cur = cur[Number(key)];
    } else if (cur !== null && typeof cur === 'object' && Object.prototype.hasOwnProperty.call(cur, key)) {
      cur = cur[key];
    } else {
      return undefined;
    }
    if (cur === undefined) return undefined;
  }
  return cur;
}

function walkSteps(j) {
  const vars = { uniq: (RUN_ID + '_' + __VU + '_' + __ITER).toLowerCase() };
  for (const s of j.steps) {
    const m = M[j.id + '__' + s.name];
    const headers = {};
    let hasContentType = false;
    for (const k of Object.keys(s.headers || {})) {
      headers[k] = sub(s.headers[k], vars, false);
      if (k.toLowerCase() === 'content-type') hasContentType = true;
    }
    if (!hasContentType) headers['Content-Type'] = 'application/json';
    const body = s.body === undefined || s.body === null ? null : JSON.stringify(subDeep(s.body, vars));

    const res = http.request(s.method, BASE + sub(s.path, vars, true), body, {
      headers: headers,
      tags: { step: j.id + '__' + s.name },
      timeout: '20s',
      responseCallback: http.expectedStatuses(s.expect_status),
    });

    let failed = res.status !== s.expect_status;
    if (!failed && s.extract) {
      try {
        const doc = res.json();
        for (const name of Object.keys(s.extract)) {
          const v = resolvePointer(doc, s.extract[name]);
          if (v === undefined || v === null) {
            failed = true;
            break;
          }
          vars[name] = typeof v === 'object' ? JSON.stringify(v) : String(v);
        }
      } catch (e) {
        failed = true;
      }
    }
    m.reqs.add(1);
    m.fails.add(failed ? 1 : 0);
    if (res.status !== 0) m.dur.add(res.timings.duration);
    if (failed) return;
  }
}

function runJourney(j) {
  walkSteps(j);
  sleep(0.05);
}

export function handleSummary(data) {
  const num = (x) => (typeof x === 'number' && isFinite(x) ? x : 0);
  const vals = (name) => (data.metrics[name] && data.metrics[name].values) || {};
  const out = [];
  for (const j of JOURNEYS) {
    for (const s of j.steps) {
      const id = j.id + '__' + s.name;
      const d = vals('ds_' + id + '_duration');
      out.push({
        journey: j.id,
        step: s.name,
        count: num(vals('ds_' + id + '_reqs').count),
        failures: num(vals('ds_' + id + '_fails').count),
        p50_ms: num(d.med),
        p95_ms: num(d['p(95)']),
        mean_ms: num(d.avg),
      });
    }
  }
  return { 'steps.json': JSON.stringify(out) };
}
