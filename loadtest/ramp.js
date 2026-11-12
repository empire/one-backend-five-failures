// The "ramp" profile: ten opens for each create, at a rate that rises one level at a time.
//
//   level L runs for STEP_S seconds at L * STEP_RPS requests per second.
//
// Each request is one iteration of an open-loop arrival-rate scenario, so a slow service
// does not slow the test down: the load keeps arriving.
import http from 'k6/http';

const BASE = __ENV.BASE || 'http://localhost:8088';
const STEP_RPS = Number(__ENV.STEP_RPS || 250);
const STEP_S = Number(__ENV.STEP_S || 10);
const LEVELS = Number(__ENV.LEVELS || 16);
const CODES = Number(__ENV.CODES || 100000); // seeded by scripts/seed.sql
const USERS = Number(__ENV.USERS || 50);

const stages = [];
for (let l = 1; l <= LEVELS; l++) {
  stages.push({duration: '1s', target: l * STEP_RPS});
  stages.push({duration: `${STEP_S - 1}s`, target: l * STEP_RPS});
}

export const options = {
  discardResponseBodies: true,
  // k6 keeps one time series per distinct tag value. The default `url` and `name` tags would hold
  // 100,000 of them (one per short link) and make k6 itself stall. Keep only what the analysis reads.
  systemTags: ['status', 'method', 'scenario'],
  scenarios: {
    ramp: {
      executor: 'ramping-arrival-rate',
      startRate: STEP_RPS,
      timeUnit: '1s',
      preAllocatedVUs: 300,
      maxVUs: 4000,
      stages,
    },
  },
};

// Log in USERS clients once. Creates need a session; opens do not.
export function setup() {
  const cookies = [];
  for (let i = 0; i < USERS; i++) {
    const res = http.post(`${BASE}/login`, JSON.stringify({user: `user${i}`}), {
      headers: {'Content-Type': 'application/json'},
      tags: {kind: 'login'},
    });
    cookies.push(res.cookies.sid[0].value);
  }
  return {cookies};
}

// Popular links are opened far more often than the rest (a heavy head and a long tail).
function pickCode() {
  const i = 1 + Math.floor(CODES * Math.pow(Math.random(), 3));
  return 's' + String(Math.min(i, CODES)).padStart(6, '0');
}

export default function (data) {
  if (Math.random() < 1 / 11) {
    const sid = data.cookies[Math.floor(Math.random() * data.cookies.length)];
    http.post(`${BASE}/links`, JSON.stringify({url: `https://example.com/new/${Math.random()}`}), {
      headers: {'Content-Type': 'application/json', Cookie: `sid=${sid}`},
      tags: {kind: 'create'},
      timeout: '3s',
    });
  } else {
    http.get(`${BASE}/${pickCode()}`, {redirects: 0, tags: {kind: 'open'}, timeout: '3s'});
  }
}
