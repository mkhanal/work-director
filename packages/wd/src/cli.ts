#!/usr/bin/env bun
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { createInterface } from 'node:readline';
import { join, resolve } from 'node:path';
import { parseArgs } from 'node:util';
import { loadCards } from '../../taste/src/build.ts';
import { compose, composeEpic, composeSlice, renderTaskBlock, type Context } from './brief.ts';
import { contextLine, workspaceContext } from './context.ts';
import { coordinateOnce, epicPlanBrief, parsePlan } from './coordinator.ts';
import { detectEditor, fileLabel, openInEditor, openLink, parseTarget } from './open.ts';
import { FeedbackSources, IllegalTransition, isEpic, Ledger, NotReady, States, WorkKinds, splitImpact, type Work, type WorkKind } from './ledger.ts';
import { installLazyspec, loadProjects, parseRunnerList, projectTemplate, Runners, type Project, type RunnerName } from './project.ts';
import { adoptCard, promotionCandidates } from './promotion.ts';
import { allRunnerNames, runnerNamed, specDir, specTemplate, writeSpec } from './runner/registry.ts';
import { run, waitFor, type Handle } from './runner/types.ts';

const root = resolve(import.meta.dir, '../../..');
const home = process.env.WD_HOME ?? join(process.env.HOME ?? '.', '.work-director');
await mkdir(home, { recursive: true });

type Args = { positional: string[]; flags: Record<string, string | boolean> };
/** util.parseArgs reads a single-dash token as short options; wd has none, so single-dash words are
 *  data (`wd impact <id> -src/…`). Move them past `--`, where parseArgs keeps them positional. */
const normalize = (argv: string[]): string[] => {
  const rest: string[] = [], singles: string[] = [];
  for (const t of argv) (t !== '--' && t.startsWith('-') && !t.startsWith('--') ? singles : rest).push(t);
  return singles.length === 0 ? rest : [...rest, '--', ...singles];
};
const parse = (argv: string[]): Args => {
  const options = {
    runner: { type: 'string' as const }, model: { type: 'string' as const }, mode: { type: 'string' as const },
    agent: { type: 'string' as const }, count: { type: 'string' as const }, tail: { type: 'string' as const },
    stack: { type: 'string' as const }, workflow: { type: 'string' as const }, verify: { type: 'string' as const },
    lazyspec: { type: 'string' as const }, kind: { type: 'string' as const }, epic: { type: 'string' as const },
    heading: { type: 'string' as const }, detail: { type: 'string' as const }, project: { type: 'string' as const },
    branch: { type: 'string' as const }, adopt: { type: 'string' as const }, source: { type: 'string' as const }, card: { type: 'string' as const },
    only: { type: 'string' as const }, timeout: { type: 'string' as const }, port: { type: 'string' as const },
    json: { type: 'boolean' as const }, all: { type: 'boolean' as const }, worktree: { type: 'boolean' as const },
    wait: { type: 'boolean' as const }, open: { type: 'boolean' as const },
    'no-code': { type: 'boolean' as const }, drop: { type: 'boolean' as const }, clear: { type: 'boolean' as const },
  };
  const r = parseArgs({ args: normalize(argv), strict: false, allowPositionals: true, options });
  return { positional: r.positionals, flags: r.values as Record<string, string | boolean> };
};
const str = (a: Args, k: string): string | undefined => { const v = a.flags[k]; return typeof v === 'string' && v !== '' ? v : undefined; };
const oneOf = <T extends readonly string[]>(v: T, x: string, what: string): T[number] => { if (!v.includes(x)) fail(`unknown ${what} ${x}; one of ${v.join(', ')}`); return x; };
const flag = (a: Args, k: string): boolean => a.flags[k] !== undefined;
const fail: (msg: string) => never = (msg) => { console.error(msg); process.exit(1); };
const ask = (q: string): Promise<string> => new Promise((resolve) => {
  const rl = createInterface({ input: process.stdin, output: process.stdout });
  rl.question(q, (a) => { rl.close(); resolve(a); });
});

const ledger = new Ledger(join(home, 'ledger.db'));
const projectsDir = process.env.WD_PROJECTS ?? join(home, 'projects');
await mkdir(projectsDir, { recursive: true });
let projects = await loadProjects(projectsDir);
const project = (name: string): Project => projects.get(name) ?? fail(`unknown project ${name}; known: ${[...projects.keys()].join(', ') || 'none'} (add ${projectsDir}/<name>.md, see projects/example.md)`);
const handle = (id: string): Handle => {
  const w = ledger.get(id), p = project(w.project);
  const runner = w.runner ?? p.runner;
  const session = w.session ?? w.claim;
  if (session === null) fail(`work ${id} has no session or claim`);
  const shared = w.parent !== null ? ledger.worktrees(w.parent).find((wt) => wt.kind === 'shared' && wt.state === 'active')?.path : undefined;
  return { runner, session, ref: w.ref, cwd: w.cwd ?? shared ?? p.path };
};
const cycle = (id: string): Context => {
  const w = ledger.get(id);
  return { decisions: ledger.events(id, 'note').map((e) => e.body), history: ledger.events(id, 'report').map((e) => `report: ${e.body.split('\n')[0]}`), roadmap: project(w.project).roadmap };
};
const briefFor = async (id: string): Promise<string> => {
  const w = ledger.get(id);
  const cards = await loadCards(join(root, 'taste/cards'));
  const p = project(w.project);
  if (isEpic(w.kind)) {
    const open = ledger.tasks(id).filter((t) => t.state !== 'done' && t.state !== 'dropped');
    return composeEpic(w, renderTaskBlock(open), p, cards, cycle(id));
  }
  if (w.parent !== null) {
    const epic = ledger.get(w.parent);
    const claims = ledger.tasks(epic.id).filter((t) => t.id !== id && t.claim !== null && t.state !== 'done' && t.state !== 'dropped');
    return composeSlice(w, epic, claims, p, cards);
  }
  return compose(w, p, cards, cycle(id));
};

const worktreesDir = join(home, 'worktrees');
await mkdir(worktreesDir, { recursive: true });
const sharedBranch = (epic: Work): string => `wd-${epic.id}`;
const branchFor = (w: Work): string => `wd-${w.id}`;

/** Create or reuse the epic's one shared worktree on branch wd-<epic>; register it in the ledger. */
async function ensureSharedWorktree(epic: Work, p: Project): Promise<string> {
  const known = ledger.worktrees(epic.id).find((wt) => wt.kind === 'shared' && wt.state === 'active');
  if (known) return known.path;
  await runGit(['worktree', 'prune'], p.path);
  const path = join(worktreesDir, `${p.name}-epic-${epic.id}`);
  let r = await runGit(['worktree', 'add', '-b', sharedBranch(epic), path], p.path);
  if (r.code !== 0) r = await runGit(['worktree', 'add', path, sharedBranch(epic)], p.path);
  if (r.code !== 0) fail(`cannot create shared worktree at ${path}: ${r.stderr.trim()}`);
  return ledger.addWorktree(epic.id, { path, branch: sharedBranch(epic), kind: 'shared' }).path;
}

/** Create a private worktree for a task, branched off the epic's shared branch. */
async function ensurePrivateWorktree(w: Work, epic: Work, p: Project): Promise<string> {
  const known = ledger.worktrees(w.id).find((wt) => wt.kind === 'private' && wt.state === 'active');
  if (known) return known.path;
  await runGit(['worktree', 'prune'], p.path);
  const path = join(worktreesDir, `${p.name}-${w.id}`);
  const r = await runGit(['worktree', 'add', '-b', branchFor(w), path, sharedBranch(epic)], p.path);
  if (r.code !== 0) fail(`cannot create private worktree at ${path}: ${r.stderr.trim()}`);
  return ledger.addWorktree(w.id, { path, branch: branchFor(w), kind: 'private' }).path;
}

function runGit(args: string[], cwd: string): Promise<{ code: number; stdout: string; stderr: string }> {
  return run(['git', ...args], cwd);
}

const a = parse(process.argv.slice(2));
const [cmd, ...rest] = a.positional;
const json = flag(a, 'json');
const out = (v: unknown, text: string): void => console.log(json ? JSON.stringify(v, null, 2) : text);

switch (cmd) {
  case 'projects': {
    const sub = rest[0];
    if (sub === 'add') {
      const name = rest[1], path = rest[2];
      if (!name || !path) fail('usage: wd projects add <name> <path> [--runner claude|opencode|codex|ao] [--mode ask|auto] [--model id] [--lazyspec y|n] [--verify cmd]');
      if (projects.has(name)) fail(`project ${name} already exists`);
      const csv = (k: string): string[] => { const v = str(a, k); return v === undefined ? [] : v.split(',').map((s) => s.trim()).filter(Boolean); };
      await writeFile(join(projectsDir, `${name}.md`), projectTemplate(name, path.replace(/^~/, process.env.HOME ?? '~'), {
        runner: str(a, 'runner'), mode: str(a, 'mode'), model: str(a, 'model'), stack: csv('stack'), workflows: csv('workflow'), verify: csv('verify'),
      }));
      projects = await loadProjects(projectsDir);
      project(name);
      const ls = str(a, 'lazyspec') ?? (flag(a, 'lazyspec') ? 'y' : undefined);
      const answer = ls ?? (process.stdin.isTTY ? await ask(`Use the director's preferred lazyspec for ${name}? [y/N] `) : 'n');
      if (answer.toLowerCase().startsWith('y')) {
        const item = installLazyspec(`Adopt the director's preferred lazyspec`);
        const w = ledger.add(name, item.title, 'evolution', item.detail, {});
        console.log(`project ${name} created (${path}); lazyspec install queued as ${w.id} (evolution — wd spawn ${w.id})`);
      } else {
        console.log(`project ${name} created (${path}); no lazyspec. Install later by adding an evolution work item.`);
      }
      break;
    }
    if (sub !== undefined) fail('usage: wd projects (list | add <name> <path>)');
    out([...projects.values()], [...projects.values()].map((p) => `${p.name}\t${p.runner}\t${p.mode}\t${p.path}`).join('\n'));
    break;
  }
  case 'models': {
    const known = await allRunnerNames();
    const name = rest[0];
    const names: string[] = name === undefined ? known : [oneOf(known, name, 'runner')];
    const rows = new Map<string, string[]>();
    for (const r of names) rows.set(r, (await (await runnerNamed(r)).models?.()) ?? []);
    const text = names.map((r) => {
      const lines = rows.get(r) ?? [];
      return lines.length ? `${r}:\n  ${lines.join('\n  ')}` : `${r}:\n  (no CLI list — pick from the provider's own picker)`;
    }).join('\n');
    out(Object.fromEntries(rows), text);
    break;
  }
  case 'runner': {
    const sub = rest[0];
    if (sub === 'list') {
      const known = await allRunnerNames();
      out(known, known.map((r) => `${r}\t${r in { claude: 1, opencode: 1, codex: 1, ao: 1 } ? 'built-in' : specDir()}`).join('\n'));
      break;
    }
    if (sub === 'add') {
      const name = rest[1], file = rest[2];
      if (!name || !file) fail('usage: wd runner add <name> <file.toml>');
      const path = await writeSpec(name, await readFile(resolve(file), 'utf8'));
      console.log(`runner ${name} added (${path}); try wd spawn <id> --runner ${name}`);
      break;
    }
    if (sub === 'init') {
      const name = rest[1];
      if (!name || !name.match(/^[A-Za-z0-9_-]+$/)) fail('usage: wd runner init <name>');
      const path = join(specDir(), `${name}.toml`);
      await mkdir(specDir(), { recursive: true });
      await writeFile(path, specTemplate(name));
      console.log(`runner spec written to ${path}; edit the commands, then wd runner add ${name} ${path}`);
      break;
    }
    fail('usage: wd runner (list | add <name> <file.toml> | init <name>)');
    break;
  }
  case 'add': {
    const [name, title] = rest;
    if (!name || !title) fail('usage: wd add <project> <title> [--kind task|evolution|workflow|goal|epic] [--epic <id>] [--heading <label>] [--detail text]');
    project(name);
    const kind: WorkKind = oneOf(WorkKinds, str(a, 'kind') ?? 'task', 'kind');
    const parent = str(a, 'epic'), heading = str(a, 'heading');
    if (parent !== undefined && isEpic(kind)) fail('an epic cannot sit under another work item');
    const w = ledger.add(name, title, kind, str(a, 'detail') ?? '', { ...(parent === undefined ? {} : { parent }), ...(heading === undefined ? {} : { heading }) });
    out(w, w.id);
    break;
  }
  case 'tasks': {
    const id = rest[0] ?? fail('usage: wd tasks <epic> [--all]');
    const epic = ledger.get(id);
    if (!isEpic(epic.kind)) fail(`${id} is not an epic`);
    const tasks = ledger.tasks(id).filter((t) => flag(a, 'all') || (t.state !== 'done' && t.state !== 'dropped'));
    out(tasks, renderTaskBlock(tasks) || 'nothing open');
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
    const id = rest[0] ?? fail('usage: wd spawn <id> [--runner claude|opencode|codex|ao|myagent…] [--model id] [--worktree]');
    const w = ledger.get(id), p = project(w.project);
    const runner = str(a, 'runner') ?? p.runner;
    let cwd = p.path;
    let runnerWorktree = false;
    if (w.parent !== null) {
      const epic = ledger.get(w.parent);
      if (flag(a, 'worktree')) cwd = await ensurePrivateWorktree(w, epic, p);
      else cwd = await ensureSharedWorktree(epic, p);
    } else {
      // standalone: let the runner manage its own worktree (claude --worktree)
      runnerWorktree = flag(a, 'worktree');
    }
    const brief = await briefFor(id);
    if (w.state === 'queued') ledger.transition(id, 'briefed');
    const rn = await runnerNamed(runner);
    const h = await rn.spawn({ cwd, name: `wd-${id} ${w.title}`.slice(0, 60), brief, agent: str(a, 'agent') ?? p.agent, model: str(a, 'model') ?? p.model, worktree: runnerWorktree });
    ledger.setSession(id, { runner, session: h.session, ref: h.ref, cwd });
    if (w.claim === null) ledger.setClaim(id, h.session);
    ledger.transition(id, 'running');
    out({ ...h, attach: rn.attachHint(h) }, `running · ${runner} · ${h.session}\nattach: ${rn.attachHint(h)}`);
    break;
  }
  case 'epic':
  case 'goal': {
    const sub = rest[0];
    // wd goal add is the entry point that records a goal kind; epic add is the generic add below.
    if (cmd === 'goal' && sub === 'add') {
      const [name, title] = rest.slice(1);
      if (!name || !title) fail('usage: wd goal add <project> <title> [--detail text]');
      project(name);
      const w = ledger.add(name, title, 'goal', str(a, 'detail') ?? '', {});
      out(w, `goal ${w.id} queued — decompose it: wd goal plan ${w.id}`);
      break;
    }
    const id = rest[1];
    const epic = id === undefined ? undefined : (() => { const w = ledger.get(id); if (!isEpic(w.kind)) fail(`${id} is not a goal or epic`); return w; })();
    const kindWord = cmd === 'goal' ? 'goal' : 'epic';
    const usage = `wd ${cmd} (plan <id> | spawn <id> | run <id> [--only|--heading] [--wait] | review <id> | status <id>)`;
    if (sub === 'plan') {
      if (epic === undefined) fail(`usage: wd ${cmd} plan <id> [--runner <runner>] [--model <id>]`);
      const p = project(epic.project);
      const rn = await runnerNamed(str(a, 'runner') ?? p.runner);
      const h = await rn.spawn({ cwd: p.path, name: `wd-${id} plan`.slice(0, 60), brief: epicPlanBrief(epic, p), agent: str(a, 'agent') ?? p.agent, model: str(a, 'model') ?? p.model });
      const tasks = await waitFor(async () => {
        const ts = parsePlan(await rn.transcript(h));
        return ts.length > 0 ? ts : undefined;
      }, 120_000);
      if (tasks === undefined) fail(`plan session ${h.session} produced no task list`);
      const rows = tasks.map((t) => ledger.add(epic.project, t.title, 'task', '', { parent: epic.id, heading: t.heading }));
      ledger.addEvent(epic.id, 'note', `plan: ${rows.length} tasks`);
      if (epic.state === 'queued') ledger.transition(epic.id, 'briefed');
      out(rows.map((w) => ({ id: w.id, heading: w.heading, title: w.title })), rows.map((w) => `${w.id}\t${w.heading}\t${w.title}`).join('\n'));
      break;
    }
    if (sub === 'spawn') {
      if (epic === undefined) fail(`usage: wd ${cmd} spawn <id> [--count n] [--model id] [--runner claude|opencode|codex|ao|claude,opencode,…]`);
      const p = project(epic.project);
      const runners = await parseRunnerList(str(a, 'runner') ?? p.runner, p.runner);
      const count = Number(str(a, 'count') ?? 1);
      if (!Number.isInteger(count) || count < 1) fail('--count must be a positive integer');
      const cwd = await ensureSharedWorktree(epic, p);
      const brief = await briefFor(id!);
      if (epic.state === 'queued') ledger.transition(id!, 'briefed');
      const sessions: string[] = [];
      const lines: string[] = [];
      let last: Handle | undefined;
      for (let i = 0; i < count; i++) {
        const runner = runners[i % runners.length]!;
        const name = `wd-${id} ${count > 1 ? (i === 0 ? `lead ${count}sessions` : `worker ${i}/${count}`) : 'solo'}`.slice(0, 60);
        const rn = await runnerNamed(runner);
        const h = await rn.spawn({ cwd, name, brief, agent: str(a, 'agent') ?? p.agent, model: str(a, 'model') ?? p.model });
        last = h;
        ledger.addEvent(id!, 'spawn', `${runner}:${h.session}`);
        sessions.push(h.session);
        lines.push(json ? JSON.stringify(h) : `${h.session}  ${rn.attachHint(h)}`);
      }
      if (last !== undefined) ledger.setSession(id!, { runner: runners[0]!, session: last.session, ref: last.ref, cwd });
      if (epic.state !== 'running') ledger.transition(id!, 'running');
      console.log(lines.join('\n'));
      if (json) console.log(JSON.stringify({ sessions }, null, 2));
      break;
    }
    if (sub === 'run') {
      if (epic === undefined) fail(`usage: wd ${cmd} run <id> [--only id,id] [--heading label] [--runner list] [--wait] [--timeout s]`);
      const p = project(epic.project);
      const runners = await parseRunnerList(str(a, 'runner') ?? p.runner, p.runner);
      const cwd = await ensureSharedWorktree(epic, p);
      const only = (str(a, 'only') ?? '').split(',').map((s) => s.trim()).filter((s) => s !== '');
      const heading = str(a, 'heading');
      const matching = (t: Work): boolean => (only.length === 0 || only.includes(t.id)) && (heading === undefined || t.heading === heading);
      const children = ledger.tasks(epic.id).filter((t) => !['done', 'dropped'].includes(t.state) && matching(t));
      if (children.length === 0) fail(`no open tasks matched${only.length > 0 ? ` (${only.join(', ')})` : heading !== undefined ? ` (heading ${heading})` : ''} — wd ${cmd} plan <id> first`);
      for (const [i, child] of children.entries()) {
        if (child.state === 'running' && child.session !== null) continue;
        const runner = runners[i % runners.length]!;
        const rn = await runnerNamed(runner);
        ledger.setCwd(child.id, cwd);
        const brief = await briefFor(child.id);
        const h = await rn.spawn({ cwd, name: `wd-${child.id} ${child.title}`.slice(0, 60), brief, agent: str(a, 'agent') ?? p.agent, model: str(a, 'model') ?? p.model });
        ledger.setSession(child.id, { runner, session: h.session, ref: h.ref, cwd });
        if (child.claim === null) ledger.setClaim(child.id, h.session);
        if (child.state === 'queued') ledger.transition(child.id, 'briefed');
        if (child.state !== 'running') ledger.transition(child.id, 'running');
        ledger.addEvent(epic.id, 'spawn', `${runner}:${h.session}`);
        console.log(`${h.session}  ${rn.attachHint(h)}`);
      }
      if (epic.state !== 'running') ledger.transition(epic.id, 'running');
      if (!flag(a, 'wait')) { out({ ok: true }, `${children.length} task(s) running on ${cwd}`); break; }
      const deadline = Date.now() + Number(str(a, 'timeout') ?? 300) * 1000;
      let escalated = false;
      do {
        const res = await coordinateOnce(epic, ledger);
        for (const k of ['answered', 'escalated', 'reviewed', 'blocked'] as const) {
          if (res[k].length > 0) console.log(`  ${k}: ${res[k].join(', ')}`);
        }
        escalated = res.escalated.length > 0;
        if (escalated) break;
        if (ledger.tasks(epic.id).filter((t) => ['running', 'needs-input'].includes(t.state)).length === 0) break;
        await Bun.sleep(1000);
      } while (Date.now() < deadline);
      const openCt = ledger.tasks(epic.id).filter((t) => ['running', 'needs-input'].includes(t.state)).length;
      out({ open: openCt }, openCt === 0
        ? `${kindWord} driven to completion — every open task closed`
        : `${kindWord} still has ${openCt} open task(s) — needs a human${escalated ? ' (a question was escalated)' : ''} or another wd ${cmd} run --wait`);
      break;
    }
    if (sub === 'review') {
      if (epic === undefined) fail(`usage: wd ${cmd} review <id>`);
      const res = await coordinateOnce(epic, ledger);
      out(res, `answered: ${res.answered.join(', ') || 'none'}\nescalated: ${res.escalated.join(', ') || 'none'}\nreviewed: ${res.reviewed.join(', ') || 'none'}\nblocked: ${res.blocked.join(', ') || 'none'}\nwaiting: ${res.waiting.join(', ') || 'none'}`);
      break;
    }
    if (sub === 'status') {
      if (epic === undefined) fail(`usage: wd ${cmd} status <id>`);
      const tasks = ledger.tasks(epic.id).filter((t) => !['done', 'dropped'].includes(t.state));
      out({ epic, open: tasks.length, tasks }, `${epic.title} (${epic.state}) · ${tasks.length} open\n${renderTaskBlock(tasks)}`);
      break;
    }
    fail(usage);
    break;
  }
  case 'send': {
    const [id, text] = rest;
    if (!id || !text) fail('usage: wd send <id> <text>');
    const h = handle(id);
    await (await runnerNamed(h.runner)).send(h, text);
    ledger.addEvent(id, 'sent', text);
    if (ledger.get(id).state !== 'running') ledger.transition(id, 'running');
    out({ ok: true }, 'sent');
    break;
  }
  case 'attach': {
    const [id, session] = rest;
    if (!id || !session) fail('usage: wd attach <id> <session> [--runner <runner>] [--model <id>] [--ref <ref>] [--cwd <dir>]');
    const w = ledger.get(id), p = project(w.project);
    const runner = str(a, 'runner') ?? w.runner ?? p.runner;
    const ref = str(a, 'ref') ?? null;
    const cwd = str(a, 'cwd') ?? w.cwd ?? (w.parent !== null ? ledger.worktrees(w.parent).find((wt) => wt.kind === 'shared' && wt.state === 'active')?.path : undefined) ?? p.path;
    const rn = await runnerNamed(runner);
    ledger.setSession(id, { runner, session, ref, cwd });
    if (w.claim === null) ledger.setClaim(id, session);
    ledger.addEvent(id, 'attach', `${runner}:${session}${ref ? ` ref=${ref}` : ''}`);
    if (w.state === 'queued' || w.state === 'briefed') ledger.transition(id, 'running');
    out({ id, runner, session, ref, cwd }, `attached ${runner}:${session} → ${w.title} (now ${ledger.get(id).state})\nattach: ${rn.attachHint({ runner, session, ref, cwd })}`);
    break;
  }
  case 'report': {
    const id = rest[0] ?? fail('usage: wd report <id> [--tail n]');
    const h = handle(id), r = await runnerNamed(h.runner);
    const texts = await r.transcript(h);
    const last = texts.at(-1) ?? '';
    const status = last.match(/STATUS:\s*(DONE|BLOCKED|NEEDS-INPUT)/)?.[1];
    const w = ledger.get(id);
    if (status && w.state !== 'done' && w.state !== 'soft-done') {
      ledger.addEvent(id, 'report', status === 'DONE' ? `DONE\n${last}` : `${status}\n${last}`);
      if (w.state === 'running') ledger.transition(id, status === 'DONE' ? 'review' : status === 'BLOCKED' ? 'blocked' : 'needs-input');
    }
    const n = Number(str(a, 'tail') ?? 1);
    out({ status: await r.status(h), report: status ?? null, messages: texts.slice(-n) }, `${await r.status(h)}${status ? ` · ${status}` : ''}\n${texts.slice(-n).join('\n---\n')}`);
    break;
  }
  case 'verify': {
    const id = rest[0] ?? fail('usage: wd verify <id>');
    const w = ledger.get(id), p = project(w.project);
    const cwd = w.cwd ?? (isEpic(w.kind) ? ledger.worktrees(id).find((wt) => wt.kind === 'shared')?.path : undefined) ?? p.path;
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
    const w = ledger.get(id);
    const codeChanged = isEpic(w.kind) ? true : w.parent !== null ? false : flag(a, 'no-code') !== true;
    try { out(ledger.softDone(id, codeChanged), 'soft-done'); } catch (e) { if (e instanceof NotReady) fail(e.message); throw e; }
    break;
  }
  case 'set': {
    const [id, to] = rest;
    if (!id || !to) fail('usage: wd set <id> <state>');
    try { out(ledger.transition(id, oneOf(States, to, 'state')), to); } catch (e) { if (e instanceof IllegalTransition) fail(e.message); throw e; }
    break;
  }
  case 'done': {
    const id = rest[0] ?? fail('usage: wd done <id>');
    try { out(ledger.transition(id, 'done'), 'done'); } catch (e) { if (e instanceof IllegalTransition) fail(e.message); throw e; }
    break;
  }
  case 'status': {
    const only = str(a, 'project');
    const items = ledger.list(only === undefined ? {} : { project: only }).filter((w) => flag(a, 'all') || !['done', 'dropped'].includes(w.state));
    const table = items.map((w) => `${w.id}\t${w.state.padEnd(11)}\t${w.project}\t${w.kind}\t${w.heading ?? ''}\t${w.title}${w.runner ? `\t${w.runner}:${w.ref ?? w.session}` : ''}`).join('\n') || 'nothing open';
    if (!json) {
      const head = [contextLine(await workspaceContext(process.cwd()))];
      for (const name of [...new Set(items.map((w) => w.project))]) {
        const p = projects.get(name);
        if (p === undefined) continue;
        const c = await workspaceContext(p.path);
        head.push(`  ${name}: ${c.branch ?? 'no commits'}${c.linked ? ` @ ${c.repo}` : ''}${c.changed.length > 0 ? ` (${c.changed.length} changed)` : ''}`);
      }
      console.log(head.join('\n'));
    }
    out(items, table);
    break;
  }
  case 'context': {
    const target = rest[0];
    const id = target !== undefined && projects.has(target) === false && ledger.has(target) ? target : undefined;
    if (id !== undefined) {
      const w = ledger.get(id), h = handle(id);
      const c = await workspaceContext(h.cwd);
      out({ id, ...h, workspace: c }, `${w.title} (${w.state})\n${contextLine(c)}\nrunner: ${h.runner} · session: ${h.session}`);
      break;
    }
    if (target !== undefined) {
      const p = project(target);
      const c = await workspaceContext(p.path);
      out({ project: p.name, path: p.path, workspace: c, runner: p.runner, mode: p.mode }, `project ${p.name}\n${contextLine(c)}\nrunner: ${p.runner} · mode: ${p.mode}`);
      break;
    }
    const c = await workspaceContext(process.cwd());
    out(c, contextLine(c));
    break;
  }
  case 'open': {
    const [maybeId, fileArg] = rest;
    if (fileArg === undefined) fail('usage: wd open [<id>] <path>[:<line>]');
    const w = maybeId !== undefined ? ledger.get(maybeId) : undefined;
    const base = w === undefined ? process.cwd() : handle(maybeId!).cwd;
    const target = parseTarget(fileArg, base);
    const editor = await detectEditor();
    const link = openLink(target);
    if (editor === null) {
      console.log(link);
      console.log(`no editor on this host — click the link or open ${target.path} manually`);
      break;
    }
    const ok = await openInEditor(editor, target);
    console.log(`${ok ? 'opened in' : 'failed to open in'} ${editor.label}: ${fileLabel(target)}`);
    console.log(link);
    break;
  }
  case 'claim': {
    const [id, whoRaw] = rest;
    if (!id) fail('usage: wd claim <id> [<who>] [--drop]');
    const w = ledger.get(id);
    if (flag(a, 'drop')) { out(ledger.setClaim(id, null), 'dropped'); break; }
    const who = whoRaw ?? w.session;
    if (who === undefined) fail('usage: wd claim <id> <who>  (or claim a spawned work with a running session)');
    out(ledger.setClaim(id, who), `claimed by ${who}`);
    break;
  }
  case 'impact': {
    const [id, op] = rest;
    if (!id) fail('usage: wd impact <id> <+path|-path> [--clear]');
    const w = ledger.get(id);
    if (flag(a, 'clear')) { out(ledger.setImpact(id, []), 'cleared'); break; }
    if (op === undefined || (op[0] !== '+' && op[0] !== '-') || op.length < 2) fail('usage: wd impact <id> <+path|-path>');
    const path = op.slice(1), cur = splitImpact(w.impact);
    const next = op[0] === '+' ? [...new Set([...cur, path])] : cur.filter((x) => x !== path);
    out(ledger.setImpact(id, next), splitImpact(ledger.get(id).impact).join('\n') || 'no impact');
    break;
  }
  case 'conflict': {
    const id = rest[0] ?? fail('usage: wd conflict <epic>');
    const epic = ledger.get(id);
    if (!isEpic(epic.kind)) fail(`${id} is not an epic`);
    const cs = ledger.conflicts(id);
    out(cs, cs.length ? cs.map((c) => `${c.a} ↔ ${c.b} on ${c.paths.join(', ')}`).join('\n') : 'no conflicts among active claims');
    break;
  }
  case 'worktree': {
    const sub = rest[0];
    if (sub === 'attach') {
      const [id, path] = rest.slice(1);
      if (!id || !path) fail('usage: wd worktree attach <id> <path> [--branch <b>]');
      const b = str(a, 'branch');
      const wt = ledger.addWorktree(id, { path: path.replace(/^~/, process.env.HOME ?? '~'), branch: b ?? null, kind: 'private' });
      out(wt, `attached private worktree ${wt.id} → ${wt.path}`);
      break;
    }
    const id = rest[1] ?? fail('usage: wd worktree (attach <id> <path> | list <id>)');
    if (sub === 'list') {
      const wts = ledger.worktrees(id);
      out(wts, wts.length ? wts.map((wt) => `${wt.id}\t${wt.kind}\t${wt.state}\t${wt.branch ?? ''}\t${wt.path}`).join('\n') : 'no worktrees');
      break;
    }
    fail('usage: wd worktree <attach|list>');
    break;
  }
  case 'merge': {
    const id = rest[0] ?? fail('usage: wd merge <id>');
    const w = ledger.get(id);
    if (w.parent === null) fail('merge is only for tasks under an epic');
    const epic = ledger.get(w.parent as string), p = project(w.project);
    const shared = ledger.worktrees(epic.id).find((wt) => wt.kind === 'shared' && wt.state === 'active');
    if (shared === undefined) fail(`epic ${epic.id} has no active shared worktree`);
    const wt = ledger.worktrees(id).find((t) => t.kind === 'private' && t.state === 'active');
    if (wt === undefined) fail(`work ${id} has no active private worktree; register one with wd worktree attach`);
    const lastVerify = ledger.events(id, 'verify').at(-1);
    if (lastVerify === undefined || !lastVerify.body.startsWith('pass')) fail('refusing merge: run wd verify <id> until it passes');
    const mine = ledger.conflicts(epic.id).filter((c) => c.a === id || c.b === id);
    if (mine.length > 0) fail(`refusing merge: conflicts with ${mine.map((c) => (c.a === id ? c.b : c.a)).join(', ')}; raise or resolve a concern first`);
    const branch = wt.branch ?? branchFor(w);
    const r = await runGit(['merge', '--no-edit', '--no-ff', branch], shared.path);
    if (r.code !== 0) {
      ledger.addEvent(id, 'note', `merge of ${branch} conflicted in ${shared.branch}; resolve then merge again`);
      fail(`merge conflicted: ${(r.stdout + r.stderr).trim().slice(0, 500)}`);
    }
    ledger.setWorktreeState(wt.id, 'merged');
    ledger.addEvent(id, 'note', `merged ${branch} into ${shared.branch}`);
    out({ merged: branch }, `merged ${branch} into ${shared.branch}`);
    break;
  }
  case 'concern': {
    const sub = rest[0];
    if (sub === 'add') {
      const [id, text] = rest.slice(1);
      if (!id || !text) fail('usage: wd concern add <work> <text>');
      const c = ledger.addConcern(id, text);
      out(c, `concern ${c.id} on ${id}`);
      break;
    }
    if (sub === 'resolve') {
      const [id, decision] = rest.slice(1);
      if (!id || !decision) fail('usage: wd concern resolve <id> <decision>');
      const c = ledger.resolveConcern(Number(id), decision);
      out(c, `concern ${c.id} resolved: ${decision}`);
      break;
    }
    if (sub === 'list') {
      const epic = rest[1];
      const cs = epic === undefined ? ledger.concerns().filter((c) => !c.resolved) : ledger.openConcerns(epic);
      out(cs, cs.length ? cs.map((c) => `${c.id}\t${c.work}\t${c.text}`).join('\n') : 'no open concerns');
      break;
    }
    fail('usage: wd concern <add|resolve|list>');
    break;
  }
  case 'scan': {
    const adopt = str(a, 'adopt');
    const cards = await loadCards(join(root, 'taste/cards'));
    const feedback = ledger.feedback();
    const distilled = ledger.distill();
    const candidates = promotionCandidates(cards, feedback);
    if (adopt !== undefined) {
      const c = candidates.find((x) => x.card.id === adopt);
      if (c === undefined) fail(`no promotion candidate ${adopt}; run wd scan to see candidates`);
      const path = await adoptCard(c.card, c.evidence.map((f) => String(f.id)), join(root, 'taste/cards'));
      out({ path }, `wrote global candidate card ${c.card.id} → ${path}\nreview it, then \`bun run build\` when adopted`);
      break;
    }
    const text = [distilled.map((x) => `${x.key} ×${x.count}\n  ${x.texts.join('\n  ')}`).join('\n') || 'no distill candidates (need ≥2 feedback on one card or project)']
      .concat('--- promotion candidates ---')
      .concat(candidates.length ? candidates.map((c) => `${c.card.id} (${c.card.category}) ×${c.evidence.length} · ${c.projects} project(s) · ${c.attached} attached\n  ${c.evidence.map((f) => f.text).slice(0, 3).join('\n  ')}`).join('\n') : 'none')
      .join('\n');
    out({ distill: distilled, promotion: candidates }, text);
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
  case 'ui': {
    const port = Number(str(a, 'port') ?? 8787);
    const ui = await import('./ui.ts');
    const srv = ui.startUi({ port, ledger, projects, cwd: process.cwd(), runCli: ui.makeCliRunner(join(import.meta.dir, 'cli.ts')) });
    const msg = `goals board: ${srv.url} (goals are entry points — track all work for a goal, status at goal level)`;
    if (flag(a, 'open')) {
      const r = await run(['open', srv.url], process.cwd());
      if (r.code !== 0) console.log(`(open failed: ${r.stderr.trim()})`);
    }
    out({ url: srv.url }, msg);
    break;
  }
  default:
    fail('wd <projects|add|tasks|brief|spawn|models|runner|epic|goal|send|attach|report|verify|pr|soft-done|set|done|status|claim|impact|conflict|worktree|merge|concern|ui|scan|events|feedback|distill> [--json]');
}