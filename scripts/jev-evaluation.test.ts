import { afterAll, beforeAll, describe, expect, test } from 'bun:test';
import { mkdtemp, readFile, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { type Attempt, type Fixtures, type Row, digestOf, main, retryDelayMs, summarize, table } from './jev-evaluation.ts';

const analysis = join(import.meta.dir, '..', 'docs', 'analysis');
const load = async (name: string): Promise<Fixtures> => JSON.parse(await readFile(join(analysis, name), 'utf8')) as Fixtures;

describe('committed fixtures', () => {
  test('v2 keeps every v1 case, label and rubric unchanged', async () => {
    const v1 = await load('2026-09-22-jev-fixtures.json'), v2 = await load('2026-09-22-jev-fixtures-v2.json');
    const v2ById = new Map(v2.cases.map((c) => [c.id, c]));
    for (const c of v1.cases) expect(v2ById.get(c.id)).toMatchObject({ task: c.task, text: c.text, expected: c.expected, set: 'v1' });
    for (const c of v2.cases.filter((c) => c.set === 'v1')) expect(c.acceptable ?? []).toEqual([]);
    expect(v2.questions).toEqual(v1.questions);
    expect(v2.repetitions).toBe(v1.repetitions);
  });

  test('every label is an option of its task and ids are unique', async () => {
    const v2 = await load('2026-09-22-jev-fixtures-v2.json');
    expect(new Set(v2.cases.map((c) => c.id)).size).toBe(v2.cases.length);
    for (const c of v2.cases) {
      const options = Object.keys(v2.questions[c.task]?.criteria ?? {});
      expect(options).toContain(c.expected);
      for (const a of c.acceptable ?? []) { expect(options).toContain(a); expect(a).not.toBe(c.expected); }
    }
  });
});

describe('retries', () => {
  test('waits on rate limits and server errors, honouring Retry-After within 1–60 s', () => {
    expect(retryDelayMs(429, '7', 1)).toBe(7000);
    expect(retryDelayMs(429, '600', 1)).toBe(60_000);
    expect(retryDelayMs(429, null, 2)).toBe(20_000);
    expect(retryDelayMs(503, 'soon', 1)).toBe(10_000);
    expect(retryDelayMs('network', null, 1)).toBe(10_000);
  });

  test('does not retry a request the gateway rejects', () => {
    for (const status of [400, 401, 403, 404]) expect(retryDelayMs(status, '1', 1)).toBeNull();
  });
});

const fixtures: Fixtures = {
  description: 'test', repetitions: 2, models: ['typesafe-ai/jev', 'x/other', 'x/refused'],
  questions: {
    routing: { type: 'choice', instructions: 'route', criteria: { user: 'u', research: 'r', director: 'd' } },
    feedback: { type: 'choice', instructions: 'tag', criteria: { comments: 'c', organisation: 'o' } },
  },
  cases: [
    { id: 'r1', task: 'routing', text: 'May I merge?', expected: 'user', set: 'v1' },
    { id: 'r2', task: 'routing', text: 'Bun version?', expected: 'research', acceptable: ['director'], set: 'v2' },
    { id: 'f1', task: 'feedback', text: 'Delete the echo comment.', expected: 'comments', set: 'v1' },
  ],
};

// Stands in for the gateway: throttles the first request, refuses one model, answers the rest.
let server: ReturnType<typeof Bun.serve>, dir = '', requests: { model: string; auth: string; body: unknown }[] = [];
const answers: Record<string, string> = { 'May I merge?': 'user', 'Bun version?': 'director', 'Delete the echo comment.': 'organisation' };
beforeAll(async () => {
  dir = await mkdtemp(join(tmpdir(), 'jev-'));
  server = Bun.serve({
    port: 0,
    async fetch(req) {
      const model = req.headers.get('ai-model-id') ?? '', body = (await req.json()) as { state: { text: string } };
      requests.push({ model, auth: req.headers.get('authorization') ?? '', body });
      if (requests.length === 1) return new Response('{"error":"slow down"}', { status: 429, headers: { 'retry-after': '1' } });
      if (model === 'x/refused') return Response.json({ error: 'unsupported' }, { status: 400 });
      const choice = answers[body.state.text] ?? 'user';
      const probabilities = model === 'typesafe-ai/jev' ? { [choice]: choice === 'organisation' ? 0.995 : 0.95 } : undefined;
      return Response.json({
        answers: { decision: { type: 'choice', choice, ...(probabilities ? { probabilities } : {}) } },
        usage: { inputTokens: 500, outputTokens: 50 },
        providerMetadata: { gateway: { cost: '0', marketCost: '0.00002', routing: { modelAttempts: [{ providerAttempts: [{ startTime: 1000, endTime: 1200 }] }] } } },
      });
    },
  });
  process.env.AI_GATEWAY_API_KEY = 'test-key';
});
afterAll(() => { server.stop(true); });

describe('run', () => {
  test('evaluates every case per model, logs each attempt, stops a refused model, and never stores the key', async () => {
    const path = join(dir, 'fixtures.json');
    await writeFile(path, JSON.stringify(fixtures));
    const lines: string[] = [];
    const summary = await main(['run', path, join(dir, 'out')], { sleep: async () => {}, log: (l) => lines.push(l) }, server.url.origin, 0);

    expect(summary.models['typesafe-ai/jev']).toMatchObject({ answered: 6, unanswered: 0, strict: { correct: 2, total: 6 }, lenient: { correct: 4, total: 6 } });
    expect(summary.models['x/refused']).toMatchObject({ answered: 0, unanswered: 6, attempts: { '400': 3 } });
    expect(lines).toContain('x/refused: stopped after 3 consecutive failures');
    const attempts = (await readFile(join(dir, 'out', 'attempts.jsonl'), 'utf8')).trim().split('\n').map((l) => JSON.parse(l) as Attempt);
    expect(attempts.filter((a) => a.status === 429)).toHaveLength(1);
    expect(attempts.filter((a) => a.status === 200)).toHaveLength(12);
    expect(requests.every((r) => r.auth === 'Bearer test-key')).toBe(true);
    for (const file of ['attempts.jsonl', 'results.jsonl', 'runs.jsonl', 'summary.json']) expect(await readFile(join(dir, 'out', file), 'utf8')).not.toContain('test-key');
  });

  test('a rerun only retries what is unanswered', async () => {
    const before = requests.length;
    await main(['run', join(dir, 'fixtures.json'), join(dir, 'out'), '--models', 'typesafe-ai/jev,x/refused'], { sleep: async () => {}, log: () => {} }, server.url.origin, 0);
    expect(requests.slice(before).map((r) => r.model)).toEqual(['x/refused', 'x/refused', 'x/refused']);
  });

  test('refuses to mix results from different fixtures', async () => {
    const changed = join(dir, 'changed.json');
    await writeFile(changed, JSON.stringify({ ...fixtures, description: 'edited' }));
    await expect(main(['run', changed, join(dir, 'out')], { sleep: async () => {}, log: () => {} }, server.url.origin, 0)).rejects.toThrow('other fixtures');
  });
});

describe('summary', () => {
  const digest = digestOf(JSON.stringify(fixtures));
  const row = (model: string, id: string, repeat: number, predicted: string, probability?: number, attemptMs = 100): Row => ({
    model, id, repeat, fixtures: digest, at: '', wallMs: attemptMs + 1000, attemptMs, predicted,
    response: { answers: { decision: { choice: predicted, ...(probability === undefined ? {} : { probabilities: { [predicted]: probability } }) } } },
  });

  test('scores strict and lenient labels, repeat agreement and each distinct mistake', () => {
    const rows = [row('m', 'r1', 0, 'user'), row('m', 'r1', 1, 'research'), row('m', 'r2', 0, 'director'), row('m', 'r2', 1, 'director'), row('m', 'f1', 0, 'comments')];
    const m = summarize(fixtures, digest, rows, []).models.m;
    expect(m).toMatchObject({ strict: { correct: 2, total: 5 }, lenient: { correct: 4, total: 5 }, repeatAgreement: { correct: 1, total: 2 }, unanswered: 1 });
    expect(m?.mistakes).toEqual([{ id: 'r1', predicted: 'research', expected: 'user', calls: 1 }, { id: 'r2', predicted: 'director', expected: 'research', calls: 2 }]);
    expect(m?.byTaskAndSet['routing/v2']).toEqual({ strict: { correct: 0, total: 2 }, lenient: { correct: 2, total: 2 } });
  });

  test('reports what a confidence cut-off keeps and how much of it is right', () => {
    const rows = [row('m', 'r1', 0, 'user', 0.999), row('m', 'r1', 1, 'research', 0.995), row('m', 'r2', 0, 'research', 0.95), row('m', 'f1', 0, 'comments', 0.5)];
    expect(summarize(fixtures, digest, rows, []).models.m?.confidence).toEqual([
      { cut: 0.9, kept: 3, correct: 2, coverage: 0.75 },
      { cut: 0.99, kept: 2, correct: 1, coverage: 0.5 },
    ]);
  });

  test('keeps the last answer when a resumed run answered a case twice, and ignores errors and other fixtures', () => {
    const rows: Row[] = [row('m', 'r1', 0, 'research'), row('m', 'r1', 0, 'user'), { model: 'm', id: 'r2', repeat: 0, fixtures: digest, at: '', wallMs: 1, attemptMs: null, error: 'HTTP 500' }, { ...row('m', 'f1', 0, 'comments'), fixtures: 'other' }];
    expect(summarize(fixtures, digest, rows, []).models.m).toMatchObject({ answered: 1, strict: { correct: 1, total: 1 } });
  });

  test('counts an answer outside the options as invalid and wrong', () => {
    const m = summarize(fixtures, digest, [row('m', 'r1', 0, 'banana')], []).models.m;
    expect(m).toMatchObject({ invalidChoice: 1, strict: { correct: 0, total: 1 } });
  });

  test('latency percentiles separate one attempt from the wait including retries', () => {
    const rows = [10, 20, 30, 40].map((ms, i) => row('m', ['r1', 'r2', 'f1', 'r1'][i] ?? 'r1', i === 3 ? 1 : 0, 'user', undefined, ms));
    expect(summarize(fixtures, digest, rows, []).models.m).toMatchObject({ attemptMs: { p50: 30, p95: 40 }, wallMsIncludingRetries: { p50: 1030, p95: 1040 } });
  });

  test('the comparison table has one row per model', () => {
    const t = table(summarize(fixtures, digest, [row('a', 'r1', 0, 'user'), row('b', 'f1', 0, 'organisation')], []));
    expect(t.split('\n')).toHaveLength(4);
    expect(t).toContain('| a | 1/1 (100%) |');
  });
});
