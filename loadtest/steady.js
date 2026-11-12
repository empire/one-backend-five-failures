// The "steady" profile: the same ten-to-one mix at a fixed rate, for the demos
// (a second app with in-memory sessions, killing an app mid-test).
//
//   RATE requests per second, for DURATION_S seconds.
import {login, oneRequest} from './lib.js';

const RATE = Number(__ENV.RATE || 500);
const DURATION_S = Number(__ENV.DURATION_S || 40);

export const options = {
  discardResponseBodies: true,
  systemTags: ['status', 'method', 'scenario'], // see ramp.js: no per-link series
  scenarios: {
    steady: {
      executor: 'constant-arrival-rate',
      rate: RATE,
      timeUnit: '1s',
      duration: `${DURATION_S}s`,
      preAllocatedVUs: 1500, // a request stuck for a second at 1,000 req/s needs 1,000 clients; the load generator must not run out
      maxVUs: 3000,
    },
  },
};

export const setup = login;
export default oneRequest;
