/*
 * Gateway relay load profile (k6).
 *
 * Usage (PowerShell):
 *   $env:GW_BASE_URL = 'http://localhost:3000'
 *   $env:GW_API_TOKEN = 'sk-<root-or-test-key>'
 *   k6 run perf/k6-chat-completions.js
 *
 * Prerequisites:
 * - The gateway points at the in-process mock upstream for the tested
 *   channel (no real OpenAI/Claude traffic, no cost, no rate limits).
 * - GW_API_TOKEN must be a valid key; placeholder tokens yield 401 and
 *   invalidate the run.
 * - Stream and non-stream run as separate scenarios so their P99 latencies
 *   never contaminate each other.
 */
import http from 'k6/http';
import { check } from 'k6';

const BASE_URL = __ENV.GW_BASE_URL || 'http://localhost:3000';
const TOKEN = __ENV.GW_API_TOKEN || '';
const MODEL = __ENV.GW_MODEL || 'gpt-3.5-turbo';
const VUS = parseInt(__ENV.GW_VUS || '100', 10);
const DURATION = __ENV.GW_DURATION || '60s';

export const options = {
  scenarios: {
    stream: {
      executor: 'constant-vus',
      vus: VUS,
      duration: DURATION,
      exec: 'chatStream',
    },
    nonstream: {
      executor: 'constant-vus',
      vus: VUS,
      duration: DURATION,
      exec: 'chatJson',
    },
  },
  thresholds: {
    'http_req_failed{scenario:stream}': ['rate<0.01'],
    'http_req_failed{scenario:nonstream}': ['rate<0.01'],
  },
};

function headers() {
  return {
    'Content-Type': 'application/json',
    Authorization: `Bearer ${TOKEN}`,
  };
}

export function chatStream() {
  const payload = JSON.stringify({
    model: MODEL,
    stream: true,
    messages: [{ role: 'user', content: 'ping' }],
  });
  const res = http.post(`${BASE_URL}/v1/chat/completions`, payload, {
    headers: headers(),
  });
  check(res, {
    'stream status is 200': (r) => r.status === 200,
    'stream ends with DONE': (r) => (r.body || '').includes('[DONE]'),
  });
}

export function chatJson() {
  const payload = JSON.stringify({
    model: MODEL,
    stream: false,
    messages: [{ role: 'user', content: 'ping' }],
  });
  const res = http.post(`${BASE_URL}/v1/chat/completions`, payload, {
    headers: headers(),
  });
  check(res, {
    'json status is 200': (r) => r.status === 200,
    'json has finish_reason': (r) =>
      (r.body || '').includes('finish_reason'),
  });
}
