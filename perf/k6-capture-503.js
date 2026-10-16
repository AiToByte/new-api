/*
 * Failure-body capture helper (not for benchmarking).
 * Runs hard concurrency for a short window and prints the first few
 * non-2xx response bodies so gateway 503s can be attributed.
 *
 * Usage (PowerShell):
 *   $env:GW_BASE_URL='http://localhost:3002'
 *   $env:GW_API_TOKEN='<test-key>'
 *   $env:GW_MODEL='mock-gpt'
 *   k6 run perf/k6-capture-503.js
 */
import http from 'k6/http';

const BASE_URL = __ENV.GW_BASE_URL || 'http://localhost:3000';
const TOKEN = __ENV.GW_API_TOKEN || '';
const MODEL = __ENV.GW_MODEL || 'mock-gpt';

export const options = {
  scenarios: {
    storm: {
      executor: 'constant-vus',
      vus: 100,
      duration: '25s',
    },
  },
};

let printed = 0;

export default function () {
  const payload = JSON.stringify({
    model: MODEL,
    stream: false,
    messages: [{ role: 'user', content: 'ping' }],
  });
  const res = http.post(`${BASE_URL}/v1/chat/completions`, payload, {
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${TOKEN}`,
    },
  });
  if (res.status !== 200 && printed < 3) {
    printed += 1;
    console.log(
      `FAIL status=${res.status} body=${(res.body || '').substring(0, 500)}`
    );
  }
}
