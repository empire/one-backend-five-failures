// What every load profile sends: ten opens for each create, from clients that are already logged in.
import http from 'k6/http';

export const BASE = __ENV.BASE || 'http://localhost:8088';
const CODES = Number(__ENV.CODES || 100000); // seeded by scripts/seed.sql
const USERS = Number(__ENV.USERS || 50);

// Log in USERS clients once. Creates need a session; opens do not.
export function login() {
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

// One request: a create one time in eleven, an open otherwise.
export function oneRequest(data) {
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
