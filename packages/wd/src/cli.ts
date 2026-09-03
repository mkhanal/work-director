#!/usr/bin/env bun
import { mkdir } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { loadCards } from '../../taste/src/build.ts';
import { compose } from './brief.ts';
import { FeedbackSources, IllegalTransition, Ledger, NotReady, States, WorkKinds, type State, type WorkKind } from './ledger.ts';
import { loadProjects, Runners, type Project, type RunnerName } from './project.ts';
import { runnerNamed } from './runner/index.ts';
import { run, type Handle } from './runner/types.ts';

const root = resolve(import.meta.dir, '../../..');
const home = process.env.WD_HOME ?? join(process.env.HOME ?? '.', '.work-director');
await mkdir(home, { recursive: true });

type Args = { positional: string[]; flags: Map<string, string | true> };
const parse = (argv: string[]): Args => {
  const a: Args = { positional: [], flags: new Map() };
  for (let i = 0; i < argv.length; i++) {
    const t = argv[i] ?? '';
    if (!t.startsWith('--')) { a.positional.push(t); continue; }
    const next = argv[i + 1];
    if (next !== undefined && !next.startsWith('--')) { a.flags.set(t.slice(2), next); i++; } else a.flags.set(t.slice(2), true);
  }
  return a;
};
const str = (a: Args, k: string): string | undefined => { const v = a.flags.get(k); return typeof v === 'string' ? v : undefined; };
const oneOf = <T extends readonly string[]>(v: T, x: string, what: string): T[number] => { if (!v.includes(x)) fail(`unknown ${what} ${x}; one of ${v.join(', ')}`); return x; };
const fail: (msg: string) => never = (msg) => { console.error(msg); process.exit(1); };

const ledger = new Ledger(join(home, 'ledger.db'));
const projectsDir = process.env.WD_PROJECTS ?? join(home, 'projects');
await mkdir(projectsDir, { recursive: true });
const projects = await loadProjects(projectsDir);
const project = (name: string): Project => projects.get(name) ?? fail(`unknown project ${name}; known: ${[...projects.keys()].join(', ') || 'none'} (add ${projectsDir}/<name>.md, see projects/example.md)`);
const handle = (id: string): Handle => {
  const w = ledger.get(id);
  if (w.runner === null || w.session === null || w.cwd === null) fail(`work ${id} has no session`);
  return { runner: oneOf(Runners, w.runner ?? '', 'runner'), session: w.session ?? '', ref: w.ref, cwd: w.cwd ?? '' };
};
const briefFor = async (id: string): Promise<string> => {
  const w = ledger.get(id);
  return compose(w, project(w.project), await loadCards(join(root, 'taste/cards')));
};

const a = parse(process.argv.slice(2));
const [cmd, ...rest] = a.positional;
const json = a.flags.get('json') === true;
const out = (v: unknown, text: string): void => console.log(json ? JSON.stringify(v, null, 2) : text);

switch (cmd) {
  case 'projects': {
    out([...projects.values()], [...projects.values()].map((p) => `${p.name}\t${p.runner}\t${p.mode}\t${p.path}`).join('\n'));
    break;
  }
  case 'add': {
    const [name, title] = rest;
    if (!name || !title) fail('usage: wd add <project> <title> [--kind task|evolution|workflow] [--detail text]');
    project(name);
    const kind: WorkKind = oneOf(WorkKinds, str(a, 'kind') ?? 'task', 'kind');
    const w = ledger.add(name, title, kind, str(a, 'detail') ?? '');
    out(w, w.id);
    break;
  }
  case 'brief': {
    const id = rest[0] ?? fail('usage: wd brief <id>');
    const text = await briefFor(id);
    if (ledger.get(id).state === 'queued') ledger.transition(id, 'briefed');
    console.log(text);
    break;
  }
  case 'spawn': {
    const id = rest[0] ?? fail('usage: wd spawn <id> [--runner claude|opencode|ao] [--worktree]');
    const w = ledger.get(id), p = project(w.project);
    const runner: RunnerName = oneOf(Runners, str(a, 'runner') ?? p.runner, 'runner');
    const brief = await briefFor(id);
    if (w.state === 'queued') ledger.transition(id, 'briefed');
    const h = await runnerNamed(runner).spawn({ cwd: p.path, name: `wd-${id} ${w.title}`.slice(0, 60), brief, agent: str(a, 'agent') ?? p.agent, worktree: a.flags.get('worktree') === true });
    ledger.setSession(id, { runner, session: h.session, ref: h.ref, cwd: h.cwd });
    ledger.transition(id, 'running');
    out({ ...h, attach: runnerNamed(runner).attachHint(h) }, `running · ${runner} · ${h.session}\nattach: ${runnerNamed(runner).attachHint(h)}`);
    break;
  }
  case 'send': {
    const [id, text] = rest;
    if (!id || !text) fail('usage: wd send <id> <text>');
    const h = handle(id);
    await runnerNamed(h.runner).send(h, text);
    ledger.addEvent(id, 'sent', text);
    if (ledger.get(id).state !== 'running') ledger.transition(id, 'running');
    out({ ok: true }, 'sent');
    break;
  }
  case 'report': {
    const id = rest[0] ?? fail('usage: wd report <id> [--tail n]');
    const h = handle(id), r = runnerNamed(h.runner);
    const texts = await r.transcript(h);
    const last = texts.at(-1) ?? '';
    const status = last.match(/STATUS:\s*(DONE|BLOCKED|NEEDS-INPUT)/)?.[1];
    if (status && ledger.get(id).state === 'running') {
      ledger.addEvent(id, 'report', status === 'DONE' ? `DONE\n${last}` : `${status}\n${last}`);
      ledger.transition(id, status === 'DONE' ? 'review' : status === 'BLOCKED' ? 'blocked' : 'needs-input');
    }
    const n = Number(str(a, 'tail') ?? 1);
    out({ status: await r.status(h), report: status ?? null, messages: texts.slice(-n) }, `${await r.status(h)}${status ? ` · ${status}` : ''}\n${texts.slice(-n).join('\n---\n')}`);
    break;
  }
  case 'verify': {
    const id = rest[0] ?? fail('usage: wd verify <id>');
    const w = ledger.get(id), p = project(w.project), cwd = w.cwd ?? p.path;
    const results: string[] = [];
    let pass = true;
    for (const cmdline of p.verify) {
      const r = await run(['bash', '-lc', cmdline], cwd);
      pass &&= r.code === 0;
      results.push(`${cmdline} → ${r.code}\n${(r.stdout + r.stderr).trim().split('\n').slice(-5).join('\n')}`);
    }
    const body = `${pass ? 'pass' : 'fail'}\n${results.join('\n')}`;
    ledger.addEvent(id, 'verify', body);
    out({ pass, results }, body);
    if (!pass) process.exit(1);
    break;
  }
  case 'pr': {
    const [id, url] = rest;
    if (!id || !url) fail('usage: wd pr <id> <url>');
    ledger.addEvent(id, 'pr', url);
    out({ ok: true }, 'recorded');
    break;
  }
  case 'soft-done': {
    const id = rest[0] ?? fail('usage: wd soft-done <id> [--no-code]');
    try { out(ledger.softDone(id, a.flags.get('no-code') !== true), 'soft-done'); } catch (e) { if (e instanceof NotReady) fail(e.message); throw e; }
    break;
  }
  case 'set': {
    const [id, to] = rest;
    if (!id || !to) fail('usage: wd set <id> <state>');
    try { out(ledger.transition(id, oneOf(States, to, 'state') as State), to); } catch (e) { if (e instanceof IllegalTransition) fail(e.message); throw e; }
    break;
  }
  case 'done': {
    const id = rest[0] ?? fail('usage: wd done <id>');
    try { out(ledger.transition(id, 'done'), 'done'); } catch (e) { if (e instanceof IllegalTransition) fail(e.message); throw e; }
    break;
  }
  case 'status': {
    const only = str(a, 'project');
    const items = ledger.list(only === undefined ? {} : { project: only }).filter((w) => a.flags.get('all') === true || !['done', 'dropped'].includes(w.state));
    out(items, items.map((w) => `${w.id}\t${w.state.padEnd(11)}\t${w.project}\t${w.kind}\t${w.title}${w.runner ? `\t${w.runner}:${w.ref ?? w.session}` : ''}`).join('\n') || 'nothing open');
    break;
  }
  case 'events': {
    const id = rest[0] ?? fail('usage: wd events <id>');
    const ev = ledger.events(id);
    out(ev, ev.map((e) => `${e.at}\t${e.kind}\t${e.body.split('\n')[0]}`).join('\n'));
    break;
  }
  case 'feedback': {
    const [sub, text] = rest;
    if (sub === 'add') {
      if (!text) fail('usage: wd feedback add <text> [--project p] [--card c] [--source director|note|attached]');
      const source = str(a, 'source'), proj = str(a, 'project'), card = str(a, 'card');
      const f = ledger.addFeedback(text, { ...(proj === undefined ? {} : { project: proj }), ...(card === undefined ? {} : { card }), ...(source === undefined ? {} : { source: oneOf(FeedbackSources, source, 'source') }) });
      out(f, String(f.id));
    } else {
      const all = ledger.feedback();
      out(all, all.map((f) => `${f.id}\t${f.source}\t${f.card ?? f.project ?? '-'}\t${f.text}`).join('\n') || 'no feedback');
    }
    break;
  }
  case 'distill': {
    const c = ledger.distill();
    out(c, c.map((x) => `${x.key} ×${x.count}\n  ${x.texts.join('\n  ')}`).join('\n') || 'no candidates (need ≥2 feedback on one card or project)');
    break;
  }
  default:
    fail('wd <projects|add|brief|spawn|send|report|verify|pr|soft-done|set|done|status|events|feedback|distill> [--json]');
}
