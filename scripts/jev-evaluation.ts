#!/usr/bin/env bun
// Runs frozen classification fixtures against evaluation models through Vercel AI Gateway.
// bun scripts/jev-evaluation.ts run <fixtures.json> <out-dir> [--models a,b]
// bun scripts/jev-evaluation.ts analyze <fixtures.json> <out-dir>
// Key: AI_GATEWAY_API_KEY, else ~/.work-director/secrets/ai-gateway-key. It is never written to <out-dir>.
import { createHash } from 'node:crypto';
import { appendFileSync, existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';

export type Question = { type: 'choice'; instructions: string; criteria: Record<string, string> };
export type Case = { id: string; task: string; text: string; expected: string; acceptable?: string[]; set?: string };
export type Fixtures = { description: string; repetitions: number; models?: string[]; cases: Case[]; questions: Record<string, Question> };
type Probabilities = Record<string, number>;
type Gateway = { cost?: string; marketCost?: string; routing?: { modelAttempts?: { providerAttempts?: { startTime: number; endTime: number }[] }[] } };
export type Response = {
  answers?: { decision?: { choice?: string; probabilities?: Probabilities } };
  usage?: { inputTokens?: number; outputTokens?: number };
  providerMetadata?: { gateway?: Gateway };
};
export type Row = {
  model: string; id: string; repeat: number; fixtures: string; at: string;
  wallMs: number; attemptMs: number | null; predicted?: string; response?: Response; error?: string;
};
export type Attempt = { model: string; id: string; repeat: number; attempt: number; status: number | 'network'; ms: number; at: string; retryAfter?: string; detail?: string };

export const GATEWAY = 'https://ai-gateway.vercel.sh/v4/ai';
const MAX_ATTEMPTS = 4;
const STOP_AFTER_FAILURES = 3;

/** Milliseconds to wait before retrying, or null when the failure will not go away by waiting. */
export function retryDelayMs(status: number | 'network', retryAfter: string | null, attempt: number): number | null {
  if (status !== 'network' && status !== 429 && status < 500) return null;
  const seconds = retryAfter !== null && /^\d+(\.\d+)?$/.test(retryAfter) ? Number(retryAfter) : 5 * 2 ** attempt;
  return Math.min(60, Math.max(1, seconds)) * 1000;
}

export type Io = {
  fetch: typeof fetch; sleep: (ms: number) => Promise<void>; now: () => number;
  attempt: (a: Attempt) => void; result: (r: Row) => void; log: (line: string) => void;
};

type Outcome = { response: Response | null; wallMs: number; attemptMs: number | null; error?: string };

async function evaluateCase(io: Io, url: string, key: string, model: string, c: Case, repeat: number, question: Question): Promise<Outcome> {
  const body = JSON.stringify({ state: { text: c.text }, questions: { decision: question } });
  const headers = {
    authorization: `Bearer ${key}`, 'content-type': 'application/json', 'ai-gateway-protocol-version': '0.0.1',
    'ai-evaluation-model-specification-version': '4', 'ai-model-id': model,
  };
  const wallStart = io.now();
  let error = '';
  for (let attempt = 1; attempt <= MAX_ATTEMPTS; attempt++) {
    const start = io.now(), at = new Date().toISOString();
    let status: number | 'network', retryAfter: string | null = null;
    try {
      const res = await io.fetch(`${url}/evaluation-model`, { method: 'POST', headers, body });
      const text = await res.text();
      status = res.status; retryAfter = res.headers.get('retry-after');
      const ms = io.now() - start;
      if (res.ok) {
        io.attempt({ model, id: c.id, repeat, attempt, status, ms, at });
        return { response: JSON.parse(text) as Response, wallMs: io.now() - wallStart, attemptMs: ms };
      }
      error = `HTTP ${status}: ${text.slice(0, 300)}`;
      io.attempt({ model, id: c.id, repeat, attempt, status, ms, at, detail: text.slice(0, 300), ...(retryAfter === null ? {} : { retryAfter }) });
    } catch (e) {
      status = 'network'; error = `network: ${String(e).slice(0, 300)}`;
      io.attempt({ model, id: c.id, repeat, attempt, status, ms: io.now() - start, at, detail: error });
    }
    const delay = retryDelayMs(status, retryAfter, attempt);
    if (delay === null || attempt === MAX_ATTEMPTS) break;
    await io.sleep(delay);
  }
  return { response: null, wallMs: io.now() - wallStart, attemptMs: null, error };
}

/** One model, every case and repetition in file order. Skips cases already answered; stops a model the gateway keeps refusing. */
export async function runModel(io: Io, opts: { url: string; key: string; model: string; fixtures: Fixtures; digest: string; done: Set<string>; pacingMs: number }): Promise<void> {
  const { url, key, model, fixtures, digest, done, pacingMs } = opts;
  let failures = 0;
  for (let repeat = 0; repeat < fixtures.repetitions; repeat++) {
    for (const c of fixtures.cases) {
      if (done.has(`${model}|${c.id}|${repeat}`)) continue;
      const question = fixtures.questions[c.task];
      if (question === undefined) throw new Error(`case ${c.id}: no question for task ${c.task}`);
      const out = await evaluateCase(io, url, key, model, c, repeat, question);
      const row: Row = { model, id: c.id, repeat, fixtures: digest, at: new Date().toISOString(), wallMs: out.wallMs, attemptMs: out.attemptMs };
      if (out.response === null) { row.error = out.error ?? 'unknown'; failures++; } else {
        failures = 0;
        row.response = out.response;
        const choice = out.response.answers?.decision?.choice;
        if (choice !== undefined) row.predicted = choice;
      }
      io.result(row);
      io.log(`${model} ${c.id}#${repeat} ${row.predicted ?? row.error}`);
      if (failures === STOP_AFTER_FAILURES) { io.log(`${model}: stopped after ${STOP_AFTER_FAILURES} consecutive failures`); return; }
      await io.sleep(pacingMs);
    }
  }
}

const percentile = (values: number[], q: number): number | null => {
  const sorted = [...values].sort((a, b) => a - b);
  return sorted.length === 0 ? null : sorted[Math.min(sorted.length - 1, Math.floor(sorted.length * q))] ?? null;
};
const round = (n: number | null): number | null => (n === null ? null : Math.round(n));
type Score = { correct: number; total: number };

export type ModelSummary = {
  answered: number; unanswered: number; invalidChoice: number;
  strict: Score; lenient: Score; byTaskAndSet: Record<string, { strict: Score; lenient: Score }>;
  repeatAgreement: Score; mistakes: { id: string; predicted: string; expected: string; calls: number }[];
  attemptMs: { p50: number | null; p95: number | null }; wallMsIncludingRetries: { p50: number | null; p95: number | null };
  providerMs: { p50: number | null };
  attempts: Record<string, number>; inputTokens: number; outputTokens: number; marketCostUsd: number; billedCostUsd: number;
  confidence: { cut: number; kept: number; correct: number; coverage: number }[] | null;
};
export type Summary = { fixtures: string; models: Record<string, ModelSummary> };

export function summarize(fixtures: Fixtures, digest: string, rows: Row[], attempts: Attempt[]): Summary {
  const cases = new Map(fixtures.cases.map((c) => [c.id, c]));
  const caseOf = (id: string): Case => { const c = cases.get(id); if (c === undefined) throw new Error(`unknown case ${id}`); return c; };
  const models: Record<string, ModelSummary> = {};
  for (const model of [...new Set(rows.map((r) => r.model))].sort()) {
    const mine = rows.filter((r) => r.model === model && r.fixtures === digest);
    const latest = new Map<string, Row>(); // a resumed run can answer a case twice; the last answer counts
    for (const r of mine) if (r.response !== undefined) latest.set(`${r.id}|${r.repeat}`, r);
    const answered = [...latest.values()];
    const hit = (r: Row, lenient: boolean): boolean => {
      const c = caseOf(r.id);
      return r.predicted === c.expected || (lenient && r.predicted !== undefined && (c.acceptable ?? []).includes(r.predicted));
    };
    const score = (subset: Row[], lenient = false): Score => ({ correct: subset.filter((r) => hit(r, lenient)).length, total: subset.length });
    const byTaskAndSet: ModelSummary['byTaskAndSet'] = {};
    for (const task of Object.keys(fixtures.questions)) for (const set of [...new Set(fixtures.cases.map((c) => c.set ?? 'all'))]) {
      const subset = answered.filter((r) => caseOf(r.id).task === task && (caseOf(r.id).set ?? 'all') === set);
      if (subset.length > 0) byTaskAndSet[`${task}/${set}`] = { strict: score(subset), lenient: score(subset, true) };
    }
    const byCase = new Map<string, string[]>();
    for (const r of answered) byCase.set(r.id, [...(byCase.get(r.id) ?? []), r.predicted ?? '∅']);
    const complete = [...byCase.values()].filter((p) => p.length === fixtures.repetitions);
    const mistakes = new Map<string, ModelSummary['mistakes'][number]>();
    for (const r of answered) if (!hit(r, false)) {
      const predicted = r.predicted ?? '∅', k = `${r.id}|${predicted}`;
      const m = mistakes.get(k) ?? { id: r.id, predicted, expected: caseOf(r.id).expected, calls: 0 };
      m.calls++; mistakes.set(k, m);
    }
    const gateway = answered.map((r) => r.response?.providerMetadata?.gateway ?? {});
    const providerMs = gateway.flatMap((g) => g.routing?.modelAttempts?.at(-1)?.providerAttempts?.at(-1) ?? []).map((p) => p.endTime - p.startTime);
    const probabilityOf = (r: Row): number | undefined => (r.predicted === undefined ? undefined : r.response?.answers?.decision?.probabilities?.[r.predicted]);
    const withProbability = answered.filter((r) => probabilityOf(r) !== undefined);
    const tried = attempts.filter((a) => a.model === model);
    const statuses: Record<string, number> = {};
    for (const a of tried) statuses[String(a.status)] = (statuses[String(a.status)] ?? 0) + 1;
    models[model] = {
      answered: answered.length,
      unanswered: fixtures.cases.length * fixtures.repetitions - answered.length,
      invalidChoice: answered.filter((r) => r.predicted === undefined || !(r.predicted in (fixtures.questions[caseOf(r.id).task]?.criteria ?? {}))).length,
      strict: score(answered), lenient: score(answered, true), byTaskAndSet,
      repeatAgreement: { correct: complete.filter((p) => new Set(p).size === 1).length, total: complete.length },
      mistakes: [...mistakes.values()].sort((a, b) => a.id.localeCompare(b.id)),
      attemptMs: { p50: round(percentile(answered.flatMap((r) => r.attemptMs ?? []), 0.5)), p95: round(percentile(answered.flatMap((r) => r.attemptMs ?? []), 0.95)) },
      wallMsIncludingRetries: { p50: round(percentile(answered.map((r) => r.wallMs), 0.5)), p95: round(percentile(answered.map((r) => r.wallMs), 0.95)) },
      providerMs: { p50: round(percentile(providerMs, 0.5)) },
      attempts: statuses,
      inputTokens: answered.reduce((n, r) => n + (r.response?.usage?.inputTokens ?? 0), 0),
      outputTokens: answered.reduce((n, r) => n + (r.response?.usage?.outputTokens ?? 0), 0),
      marketCostUsd: gateway.reduce((n, g) => n + Number(g.marketCost ?? 0), 0),
      billedCostUsd: gateway.reduce((n, g) => n + Number(g.cost ?? 0), 0),
      confidence: withProbability.length === 0 ? null : [0.9, 0.99].map((cut) => {
        const kept = withProbability.filter((r) => (probabilityOf(r) ?? 0) >= cut);
        return { cut, kept: kept.length, correct: score(kept).correct, coverage: kept.length / answered.length };
      }),
    };
  }
  return { fixtures: digest, models };
}

const pct = (s: Score): string => (s.total === 0 ? '–' : `${s.correct}/${s.total} (${Math.round((100 * s.correct) / s.total)}%)`);

/** The comparison table the analysis note quotes. */
export function table(summary: Summary): string {
  const lines = ['| Model | Routing strict | Routing lenient | Feedback strict | Feedback lenient | Repeat agreement | p50 / p95 ms | Market cost |', '| --- | --- | --- | --- | --- | --- | --- | --- |'];
  for (const [model, m] of Object.entries(summary.models)) {
    const task = (t: string, lenient: boolean): Score => Object.entries(m.byTaskAndSet).filter(([k]) => k.startsWith(`${t}/`))
      .reduce<Score>((acc, [, v]) => { const s = lenient ? v.lenient : v.strict; return { correct: acc.correct + s.correct, total: acc.total + s.total }; }, { correct: 0, total: 0 });
    lines.push(`| ${model} | ${pct(task('routing', false))} | ${pct(task('routing', true))} | ${pct(task('feedback', false))} | ${pct(task('feedback', true))} | ${m.repeatAgreement.correct}/${m.repeatAgreement.total} | ${m.attemptMs.p50} / ${m.attemptMs.p95} | $${m.marketCostUsd.toFixed(4)} |`);
  }
  return lines.join('\n');
}

export const digestOf = (raw: string | Buffer): string => createHash('sha256').update(raw).digest('hex');
const readJsonl = <T>(path: string): T[] => (existsSync(path) ? readFileSync(path, 'utf8').split('\n').filter(Boolean).map((l) => JSON.parse(l) as T) : []);

function apiKey(): string {
  const fromEnv = process.env.AI_GATEWAY_API_KEY?.trim();
  if (fromEnv) return fromEnv;
  const file = join(homedir(), '.work-director', 'secrets', 'ai-gateway-key');
  if (existsSync(file)) return readFileSync(file, 'utf8').trim();
  throw new Error('no key: set AI_GATEWAY_API_KEY or write ~/.work-director/secrets/ai-gateway-key');
}

export async function main(argv: string[], io: Partial<Io> = {}, url = GATEWAY, pacingMs = 3200): Promise<Summary> {
  const [command, fixturesPath, out, flag, list] = argv;
  if ((command !== 'run' && command !== 'analyze') || fixturesPath === undefined || out === undefined) throw new Error('usage: run|analyze <fixtures.json> <out-dir> [--models a,b]');
  const raw = readFileSync(fixturesPath), digest = digestOf(raw), fixtures = JSON.parse(raw.toString()) as Fixtures;
  mkdirSync(out, { recursive: true });
  const results = join(out, 'results.jsonl'), attempts = join(out, 'attempts.jsonl');
  if (command === 'run') {
    const earlier = readJsonl<Row>(results);
    if (earlier.some((r) => r.fixtures !== digest)) throw new Error(`${results} came from other fixtures; use a fresh directory`);
    const done = new Set(earlier.filter((r) => r.response !== undefined).map((r) => `${r.model}|${r.id}|${r.repeat}`));
    const models = flag === '--models' && list !== undefined ? list.split(',') : fixtures.models ?? [];
    if (models.length === 0) throw new Error('no models: list them in the fixtures or pass --models');
    const full: Io = {
      fetch, sleep: (ms) => new Promise((r) => setTimeout(r, ms)), now: () => performance.now(),
      attempt: (a) => appendFileSync(attempts, `${JSON.stringify(a)}\n`), result: (r) => appendFileSync(results, `${JSON.stringify(r)}\n`),
      log: (line) => console.log(line), ...io,
    };
    const key = apiKey(), started = full.now();
    await Promise.all(models.map((model) => runModel(full, { url, key, model, fixtures, digest, done, pacingMs })));
    appendFileSync(join(out, 'runs.jsonl'), `${JSON.stringify({ finished: new Date().toISOString(), elapsedMs: Math.round(full.now() - started), models, fixtures: digest, url })}\n`);
  }
  const summary = summarize(fixtures, digest, readJsonl<Row>(results), readJsonl<Attempt>(attempts));
  writeFileSync(join(out, 'summary.json'), `${JSON.stringify(summary, null, 2)}\n`);
  return summary;
}

if (import.meta.main) {
  const summary = await main(process.argv.slice(2));
  console.log(table(summary));
}
