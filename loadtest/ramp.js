// The "ramp" profile: ten opens for each create, at a rate that rises one level at a time.
//
//   level L runs for STEP_S seconds at L * STEP_RPS requests per second.
//
// Each request is one iteration of an open-loop arrival-rate scenario, so a slow service
// does not slow the test down: the load keeps arriving.
import {login, oneRequest} from './lib.js';

const STEP_RPS = Number(__ENV.STEP_RPS || 250);
const STEP_S = Number(__ENV.STEP_S || 10);
const LEVELS = Number(__ENV.LEVELS || 16);

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

export const setup = login;
export default oneRequest;
