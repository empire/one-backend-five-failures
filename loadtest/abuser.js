// The "abuser" profile: the steady mix from 50 normal clients, plus one client that creates links in a loop.
//
//   RATE requests per second from the normal clients: with 50 clients and ten opens to each create,
//   RATE=550 is one create a second for each normal client.
//   ABUSER_RPS creates per second from the one abuser.
import http from 'k6/http';
import {BASE, login, loginAs, oneRequest} from './lib.js';

const RATE = Number(__ENV.RATE || 550);
const ABUSER_RPS = Number(__ENV.ABUSER_RPS || 20);
const DURATION_S = Number(__ENV.DURATION_S || 30);

const arrivals = (rate, vus, maxVUs, exec) => ({
  executor: 'constant-arrival-rate',
  rate,
  timeUnit: '1s',
  duration: `${DURATION_S}s`,
  preAllocatedVUs: vus,
  maxVUs,
  exec,
});

export const options = {
  discardResponseBodies: true,
  systemTags: ['status', 'method', 'scenario'], // see ramp.js: no per-link series
  scenarios: {
    normal: arrivals(RATE, 1000, 3000, 'normal'),
    abuser: arrivals(ABUSER_RPS, 100, 400, 'abuser'),
  },
};

export function setup() {
  const data = login();
  data.abuser = loginAs('abuser');
  return data;
}

export function normal(data) {
  oneRequest(data);
}

export function abuser(data) {
  http.post(`${BASE}/links`, JSON.stringify({url: `https://spam.example/${Math.random()}`}), {
    headers: {'Content-Type': 'application/json', Cookie: `sid=${data.abuser}`},
    tags: {kind: 'create'},
    timeout: '3s',
  });
}
