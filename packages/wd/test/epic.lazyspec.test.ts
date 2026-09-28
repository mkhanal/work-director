import { beforeAll, describe, expect, test } from 'bun:test';
import { chmod, mkdtemp, mkdir, readFile, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { coordinateOnce, epicPlanBrief, knownAnswer, parsePlan } from '../src/coordinator.ts';
import { IllegalTransition, Ledger, NotReady, type Work } from '../src/ledger.ts';
import type { Runner } from '../src/runner/types.ts';
import { parseProject, parseRunnerList, projectTemplate } from '../src/project.ts';

const fresh = (): Ledger => new Ledger(':memory:');
const running = (l: Ledger, epic: Work, title = 't', runner = 'claude'): Work => {
  const t = l.add('p', title, 'task', '', { parent: epic.id });
  l.setClaim(t.id, 'ses-1');
  l.setSession(t.id, { runner, session: 'ses-1', ref: null, cwd: '.' });
  l.transition(t.id, 'briefed');
  l.transition(t.id, 'running');
  return t;
};

class Stub implements Runner {
  readonly name = 'claude' as const;
  sent: string[] = [];
  constructor(private readonly lines: string[]) {}
  async spawn() { return { runner: this.name, session: 'ses-1', ref: null, cwd: '.' }; }
  async send(_: unknown, text: string) { this.sent.push(text); }
  async status() { return 'waiting' as const; }
  async transcript() { return this.lines; }
  attachHint() { return 'stub attach'; }
}

describe('A Task Lives Under An Epic And A Heading', () => {
  test('parent and heading are recorded; the task shows under tasks(epic)', () => {
    const l = fresh(), epic = l.add('p', 'Move it', 'epic');
    const t = l.add('p', 'Port dashboards', 'task', '', { parent: epic.id, heading: 'Dashboards' });
    expect(t.parent).toBe(epic.id);
    expect(t.heading).toBe('Dashboards');
    expect(l.tasks(epic.id).map((x) => x.id)).toEqual([t.id]);
  });
  test('a parent that is not an epic is rejected', () => {
    const l = fresh(), notEpic = l.add('p', 'ordinary');
    expect(() => l.add('p', 'child', 'task', '', { parent: notEpic.id })).toThrow(/not an epic/);
  });
});

describe('A Task Soft-Done Needs Only The Executor\'s Done Report', () => {
  test('task under an epic passes with just a DONE report, even with code changed', () => {
    const l = fresh(), epic = l.add('p', 'epic', 'epic'), t = l.add('p', 't', 'task', '', { parent: epic.id });
    for (const s of ['briefed', 'running', 'review'] as const) l.transition(t.id, s);
    expect(() => l.softDone(t.id, false)).toThrow(new NotReady(['DONE report']));
    l.addEvent(t.id, 'report', 'DONE\nok');
    expect(l.softDone(t.id, false).state).toBe('soft-done');
  });
});

describe('An Epic Soft-Done Requires Every Task And A Pull Request', () => {
  test('refuses listing tasks, report, verify and PR until all present', () => {
    const l = fresh(), epic = l.add('p', 'epic', 'epic');
    const keep = l.add('p', 'still open', 'task', '', { parent: epic.id });
    for (const s of ['briefed', 'running', 'review'] as const) l.transition(epic.id, s);
    expect(() => l.softDone(epic.id, true)).toThrow(new NotReady(['1 task(s) not done', 'DONE report', 'passing verify', 'pull request']));
    l.transition(keep.id, 'dropped');
    l.addEvent(epic.id, 'report', 'DONE');
    l.addEvent(epic.id, 'verify', 'pass');
    l.addEvent(epic.id, 'pr', 'https://github.com/x/pull/1');
    expect(l.softDone(epic.id, true).state).toBe('soft-done');
  });
});

describe('Claims And Impacts Are Tracked Per Task', () => {
  test('claim set and dropped; impact replaces, joins by newline, drops a path', () => {
    const l = fresh(), t = l.add('p', 't', 'task', '', { parent: l.add('p', 'e', 'epic').id });
    l.setClaim(t.id, 'ses_1');
    expect(l.get(t.id).claim).toBe('ses_1');
    l.setImpact(t.id, ['src/a', 'src/b']);
    expect(l.get(t.id).impact).toBe('src/a\nsrc/b');
    l.setImpact(t.id, ['src/a']);
    expect(l.get(t.id).impact).toBe('src/a');
    l.setClaim(t.id, null);
    expect(l.get(t.id).claim).toBeNull();
  });
});

describe('Conflicting Impacts Surface Together', () => {
  test('overlapping claimed tasks are paired; identical and contained paths count, done tasks do not', () => {
    const l = fresh(), epic = l.add('p', 'e', 'epic');
    const a = l.add('p', 'a', 'task', '', { parent: epic.id });
    const b = l.add('p', 'b', 'task', '', { parent: epic.id });
    const c = l.add('p', 'c', 'task', '', { parent: epic.id });
    const done = l.add('p', 'd', 'task', '', { parent: epic.id });
    for (const id of [a.id, b.id, c.id, done.id]) { l.setClaim(id, 's'); l.setImpact(id, ['src/shared']); }
    l.setImpact(c.id, ['src/other']);
    for (const id of [a.id, b.id, c.id]) { l.transition(id, 'briefed'); l.transition(id, 'running'); }
    for (const s of ['briefed', 'running', 'review'] as const) l.transition(done.id, s);
    l.addEvent(done.id, 'report', 'DONE');
    l.softDone(done.id, false);
    l.transition(done.id, 'done');
    const cs = l.conflicts(epic.id);
    expect(cs).toEqual([{ a: a.id, b: b.id, paths: ['src/shared'] }]);
  });
});

describe('Concerns Resolve With A Recorded Decision', () => {
  test('resolution stores the decision and a note event on the work', () => {
    const l = fresh(), epic = l.add('p', 'e', 'epic'), t = l.add('p', 't', 'task', '', { parent: epic.id });
    const c = l.addConcern(t.id, 'metabase API rate limits our sync');
    expect(l.openConcerns(epic.id).map((x) => x.id)).toEqual([c.id]);
    const r = l.resolveConcern(c.id, 'batch the sync');
    expect(r.resolved).toBe(1);
    expect(r.decision).toBe('batch the sync');
    expect(l.openConcerns(epic.id)).toEqual([]);
    expect(l.events(t.id, 'note').at(-1)?.body).toBe(`concern ${c.id} resolved: batch the sync`);
  });
});

describe('Worktrees Are Registered Shared Or Private', () => {
  test('one shared plus private worktrees are recorded active; state moves to merged', () => {
    const l = fresh(), epic = l.add('p', 'e', 'epic'), t = l.add('p', 't', 'task', '', { parent: epic.id });
    const shared = l.addWorktree(epic.id, { path: '/wt/e', branch: 'wd-e', kind: 'shared' });
    const priv = l.addWorktree(t.id, { path: '/wt/t', branch: 'wd-t', kind: 'private' });
    expect(l.worktrees(epic.id).map((w) => [w.kind, w.state, w.branch])).toEqual([['shared', 'active', 'wd-e']]);
    expect(l.setWorktreeState(priv.id, 'merged').state).toBe('merged');
    expect(l.worktrees(t.id)[0]?.state).toBe('merged');
    expect(shared.kind).toBe('shared');
  });
});

describe('An Epic Spawn Can Parcel Sessions Across Inference Providers', () => {
  test('comma list cycles over providers; empty falls back; unknown is rejected', async () => {
    expect(await parseRunnerList('claude,opencode', 'ao')).toEqual(['claude', 'opencode']);
    expect((await parseRunnerList('claude,opencode', 'ao')).length).toBe(2);
    expect(await parseRunnerList('', 'ao')).toEqual(['ao']);
    expect(await parseRunnerList('claude, claude ', 'ao')).toEqual(['claude', 'claude']);
    await expect(parseRunnerList('cursor,cursor', 'ao')).rejects.toThrow(/cursor/);
  });
});

describe('An Epic Plan Decomposes Into Headed Tasks From A Planner Session', () => {
  test('epicPlanBrief restates the goal and the project verify commands', () => {
    const epic = new Ledger(':memory:').add('p', 'Listen on a port', 'epic', 'the server must bind and answer');
    const p = parseProject(projectTemplate('p', '.', { runner: 'claude', mode: 'auto', verify: ['bun test'] }), 'projects/p.md');
    const brief = epicPlanBrief(epic, p);
    expect(brief).toContain('Listen on a port');
    expect(brief).toContain('the server must bind and answer');
    expect(brief).toContain('`bun test`');
  });
  test('parsePlan reads headings and checkboxes, ignoring prose and other bullets', () => {
    expect(parsePlan(['Let me think.', '## Analysis', '- [ ] parse the config', '## Wiring', '- [x] add the schema column', '- hello', '## Done'])).toEqual([
      { heading: 'Analysis', title: 'parse the config' },
      { heading: 'Wiring', title: 'add the schema column' },
    ]);
    expect(parsePlan([])).toEqual([]);
  });
});

describe('The Coordinator Answers Known Questions And Escalates Unknown Ones', () => {
  test('knownAnswer returns a resolved concern or decision event whose words overlap; containment wins', () => {
    const l = fresh(), g = l.add('p', 'g', 'epic');
    const c = l.addConcern(g.id, 'server port');
    l.resolveConcern(c.id, 'the server listens on port number 8080');
    expect(knownAnswer('which port number should the server listen on?', g, [], l)).toBe('the server listens on port number 8080');
    const g2 = l.add('p', 'g2', 'epic');
    l.addEvent(g2.id, 'decision', 'the answer is rising quickly');
    expect(knownAnswer('what answer is rising quickly?', g2, [], l)).toBe('the answer is rising quickly');
    const g3 = l.add('p', 'g3', 'epic');
    l.addEvent(g3.id, 'decision', 'bind the api on port 7000');
    expect(knownAnswer('port', g3, [], l)).toBe('bind the api on port 7000');
  });
  test('a running task that asks a known question is answered and stays running', async () => {
    const l = fresh(), g = l.add('p', 'g', 'epic');
    const c = l.addConcern(g.id, 'server port');
    l.resolveConcern(c.id, 'listen on port number 8080 bound to localhost');
    const t = running(l, g);
    const stub = new Stub(['ASK: which port number should the server listen on?']);
    const res = await coordinateOnce(g, l, async () => stub);
    expect(res.answered).toEqual([t.id]);
    expect(res.escalated).toEqual([]);
    expect(stub.sent).toEqual(['listen on port number 8080 bound to localhost']);
    expect(l.get(t.id).state).toBe('running');
    expect(l.events(t.id, 'answer').at(-1)?.body).toBe('listen on port number 8080 bound to localhost');
  });
  test('a question nothing answers moves the task to needs-input and is escalated, not guessed', async () => {
    const l = fresh(), g = l.add('p', 'g', 'epic');
    const t = running(l, g);
    const stub = new Stub(['ASK: which colour should the banner be?']);
    const res = await coordinateOnce(g, l, async () => stub);
    expect(res.escalated).toEqual([t.id]);
    expect(res.answered).toEqual([]);
    expect(stub.sent).toEqual([]);
    expect(l.get(t.id).state).toBe('needs-input');
  });
  test('a task awaiting a session (no claim) is reported waiting, not touched', async () => {
    const l = fresh(), g = l.add('p', 'g', 'epic');
    const t = l.add('p', 't', 'task', '', { parent: g.id });
    l.transition(t.id, 'briefed'); l.transition(t.id, 'running');
    const res = await coordinateOnce(g, l, async () => new Stub([]));
    expect(res.waiting).toEqual([t.id]);
    expect(l.get(t.id).state).toBe('running');
  });
});

describe('The Coordinator Harvests Done Reports Into Review', () => {
  test('STATUS: DONE wins a report event and review; BLOCKED moves to blocked; mid-work waits', async () => {
    const l = fresh(), g = l.add('p', 'g', 'epic');
    const done = running(l, g, 'a', 'claude'), block = running(l, g, 'b', 'opencode'), mid = running(l, g, 'c', 'codex');
    const res = await coordinateOnce(g, l, async (n) => {
      const stub = new Stub(n === 'claude' ? ['STATUS: DONE'] : n === 'opencode' ? ['STATUS: BLOCKED'] : ['still working']);
      return stub;
    });
    expect(res.reviewed).toEqual([done.id]);
    expect(res.blocked).toEqual([block.id]);
    expect(res.waiting).toEqual([mid.id]);
    expect(l.get(done.id).state).toBe('review');
    expect(l.get(block.id).state).toBe('blocked');
    expect(l.get(mid.id).state).toBe('running');
    expect(l.events(done.id, 'report').at(-1)?.body.startsWith('DONE')).toBe(true);
  });
  test('a needs-input task that already finished is harvested, not left waiting on a human', async () => {
    const l = fresh(), g = l.add('p', 'g', 'epic');
    const t = running(l, g);
    l.transition(t.id, 'needs-input');
    const res = await coordinateOnce(g, l, async () => new Stub(['STATUS: DONE']));
    expect(res.reviewed).toEqual([t.id]);
    expect(l.get(t.id).state).toBe('review');
  });
});

// ---- e2e: real CLI against a git repo with fake planner/advisor runners ----

const PKG = dirname(import.meta.dir); // packages/wd
const ROOT = dirname(dirname(PKG)); // work-director
const TESTBED = join(ROOT, 'testbed');

const runCli = async (args: string[], env: Record<string, string>, cwd: string): Promise<{ code: number; stdout: string }> => {
  const p = Bun.spawn(['bun', join(PKG, 'src', 'cli.ts'), ...args], {
    cwd,
    env: { ...process.env, ...env, PATH: `${join(TESTBED, 'runners')}:${process.env.PATH ?? ''}` },
    stdout: 'pipe',
    stderr: 'pipe',
  });
  const [out, err] = await Promise.all([new Response(p.stdout).text(), new Response(p.stderr).text()]);
  return { code: await p.exited, stdout: out + err };
};

describe('An Epic Run Spawns Open Tasks On One Shared Worktree', () => {
  let base = '', home = '', fakeState = '', repo = '';
  beforeAll(async () => {
    base = await mkdtemp(join(tmpdir(), 'wd-epic-run-'));
    home = join(base, 'home', '.work-director');
    fakeState = join(base, 'fake');
    repo = join(base, 'repo');
    for (const d of [join(home, 'projects'), join(home, 'runners'), join(home, 'worktrees'), fakeState, repo]) await mkdir(d, { recursive: true });
    const git = async (args: string[], wd = repo) => {
      const exe = Bun.which('git') ?? '/usr/bin/git';
      const p = Bun.spawn([exe, ...args], { cwd: wd, stdout: 'pipe', stderr: 'pipe' });
      const [o, e] = await Promise.all([new Response(p.stdout).text(), new Response(p.stderr).text()]);
      return { code: await p.exited, out: o, err: e };
    };
    await git(['init', '-q']);
    await git(['-c', 'user.name=wd', '-c', 'user.email=wd@wd', 'commit', '-q', '--allow-empty', '-m', 'init']);
    for (const name of ['planner', 'advisor']) {
      await chmod(join(TESTBED, 'runners', name), 0o755);
      await writeFile(join(home, 'runners', `${name}.toml`), await readFile(join(TESTBED, 'specs', `${name}.toml`), 'utf8'));
    }
  });

  test('add → plan → run waits on a question → answering it drives tasks to review', async () => {
    await writeFile(join(home, 'projects', 'wd.md'), projectTemplate('wd', repo, { runner: 'planner', mode: 'auto', verify: ['bun test'] }));
    const env = { WD_HOME: home, WD_PROJECTS: join(home, 'projects'), HOME: home, WD_FAKE_STATE: fakeState };
    const add = await runCli(['add', 'wd', 'Bind the server', '--kind', 'epic'], env, repo);
    const gid = add.stdout.trim().match(/^[0-9a-f]{8}$/)?.[0] ?? (() => { throw new Error(`no epic id in:\n${add.stdout}`); })();
    const plan = await runCli(['epic', 'plan', gid], env, repo);
    expect(plan.stdout.trim().split('\n').map((x) => x.split('\t')).filter((x) => x[0]?.match(/^[0-9a-f]{8}$/))).toHaveLength(2);
    const run = await runCli(['epic', 'run', gid, '--runner', 'advisor', '--timeout', '3', '--wait'], env, repo);
    expect(run.stdout).toContain('open task'); // escalated to a human, not guessed
    const c = await runCli(['concern', 'add', gid, 'server port'], env, repo);
    const cid = c.stdout.match(/concern (\d+) on/)?.[1];
    if (cid === undefined) throw new Error(`no concern id in:\n${c.stdout}`);
    await runCli(['concern', 'resolve', cid, 'listen on port number 8080 bound to localhost'], env, repo);
    const r1 = await runCli(['epic', 'review', gid], env, repo);
    expect(r1.stdout).toMatch(/answered:/);
    const r2 = await runCli(['epic', 'review', gid], env, repo);
    expect(r2.stdout).toMatch(/reviewed:/);
    const tasks = await runCli(['tasks', gid], env, repo);
    expect(tasks.stdout.trim().split('\n').filter((x) => x.match(/^-\s+[0-9a-f]{8}\s+review/)), `tasks out:\n${tasks.stdout}`).toHaveLength(2);
  }, 90_000);
});

describe('An Epic Run Can Work A Partial Slice', () => {
  let base = '', home = '', fakeState = '', repo = '';
  beforeAll(async () => {
    base = await mkdtemp(join(tmpdir(), 'wd-epic-slice-'));
    home = join(base, 'home', '.work-director');
    fakeState = join(base, 'fake');
    repo = join(base, 'repo');
    for (const d of [join(home, 'projects'), join(home, 'runners'), join(home, 'worktrees'), fakeState, repo]) await mkdir(d, { recursive: true });
    const git = async (args: string[], wd = repo) => {
      const exe = Bun.which('git') ?? '/usr/bin/git';
      const p = Bun.spawn([exe, ...args], { cwd: wd, stdout: 'pipe', stderr: 'pipe' });
      const [o, e] = await Promise.all([new Response(p.stdout).text(), new Response(p.stderr).text()]);
      return { code: await p.exited, out: o, err: e };
    };
    await git(['init', '-q']);
    await git(['-c', 'user.name=wd', '-c', 'user.email=wd@wd', 'commit', '-q', '--allow-empty', '-m', 'init']);
    for (const name of ['planner', 'advisor']) {
      await chmod(join(TESTBED, 'runners', name), 0o755);
      await writeFile(join(home, 'runners', `${name}.toml`), await readFile(join(TESTBED, 'specs', `${name}.toml`), 'utf8'));
    }
  });

  test('--only spawns just that task and leaves the rest open for a later conversation', async () => {
    await writeFile(join(home, 'projects', 'wd.md'), projectTemplate('wd', repo, { runner: 'planner', mode: 'auto', verify: ['bun test'] }));
    const env = { WD_HOME: home, WD_PROJECTS: join(home, 'projects'), HOME: home, WD_FAKE_STATE: fakeState };
    const gid = (await runCli(['add', 'wd', 'Bind the server', '--kind', 'epic'], env, repo)).stdout.trim();
    const plan = await runCli(['epic', 'plan', gid], env, repo);
    const ids = plan.stdout.trim().split('\n').map((x) => x.split('\t')).filter((x) => x[0]?.match(/^[0-9a-f]{8}$/)).map((x) => x[0]!);
    expect(ids).toHaveLength(2);
    // Run only the first task.
    const run = await runCli(['epic', 'run', gid, '--only', ids[0]!, '--runner', 'advisor'], env, repo);
    expect(run.stdout).toContain('1 task(s) running');
    const tasks1 = await runCli(['tasks', gid], env, repo);
    expect(tasks1.stdout).toMatch(new RegExp(`-\\s+${ids[0]}\\s+running`));
    expect(tasks1.stdout).toMatch(new RegExp(`-\\s+${ids[1]}\\s+queued`));
    // The leftover task stays open: a later conversation runs it on its own.
    const run2 = await runCli(['epic', 'run', gid, '--only', ids[1]!, '--runner', 'advisor'], env, repo);
    expect(run2.stdout).toContain('1 task(s) running');
    const tasks2 = await runCli(['tasks', gid], env, repo);
    expect(tasks2.stdout).toMatch(new RegExp(`-\\s+${ids[1]}\\s+running`));
  }, 90_000);
});

describe('An Outside Conversation Can Be Attached And Driven With The Same Llm', () => {
  let base = '', home = '', fakeState = '', repo = '';
  beforeAll(async () => {
    base = await mkdtemp(join(tmpdir(), 'wd-attach-'));
    home = join(base, 'home', '.work-director');
    fakeState = join(base, 'fake');
    repo = join(base, 'repo');
    for (const d of [join(home, 'projects'), join(home, 'runners'), join(home, 'worktrees'), fakeState, repo]) await mkdir(d, { recursive: true });
    const git = async (args: string[], wd = repo) => {
      const exe = Bun.which('git') ?? '/usr/bin/git';
      const p = Bun.spawn([exe, ...args], { cwd: wd, stdout: 'pipe', stderr: 'pipe' });
      const [o, e] = await Promise.all([new Response(p.stdout).text(), new Response(p.stderr).text()]);
      return { code: await p.exited, out: o, err: e };
    };
    await git(['init', '-q']);
    await git(['-c', 'user.name=wd', '-c', 'user.email=wd@wd', 'commit', '-q', '--allow-empty', '-m', 'init']);
    for (const name of ['planner', 'advisor']) {
      await chmod(join(TESTBED, 'runners', name), 0o755);
      await writeFile(join(home, 'runners', `${name}.toml`), await readFile(join(TESTBED, 'specs', `${name}.toml`), 'utf8'));
    }
  });

  test('a session started outside is bound as the live session, then the coordinator drives that same conversation', async () => {
    await writeFile(join(home, 'projects', 'wd.md'), projectTemplate('wd', repo, { runner: 'planner', mode: 'auto', verify: ['bun test'] }));
    const env = { WD_HOME: home, WD_PROJECTS: join(home, 'projects'), HOME: home, WD_FAKE_STATE: fakeState };
    const gid = (await runCli(['add', 'wd', 'Bind the server', '--kind', 'epic'], env, repo)).stdout.trim();
    const tid = (await runCli(['add', 'wd', 'wire the port', '--kind', 'task', '--epic', gid], env, repo)).stdout.trim();
    // The conversation already exists in the provider (started outside the director).
    const att = await runCli(['attach', tid, 'advisor-s2', '--runner', 'advisor'], env, repo);
    expect(att.stdout).toMatch(/advisor:advisor-s2 → wire the port \(now running\)/);
    expect(att.stdout).toContain('attach:');
    // A decision is recorded on the epic…
    const c = await runCli(['concern', 'add', gid, 'server port'], env, repo);
    const cid = c.stdout.match(/concern (\d+) on/)?.[1];
    if (cid === undefined) throw new Error(`no concern id in:\n${c.stdout}`);
    await runCli(['concern', 'resolve', cid, 'listen on port number 8080 bound to localhost'], env, repo);
    // …and the coordinator drives the same session: answers its question, then harvests DONE.
    const r1 = await runCli(['epic', 'review', gid], env, repo);
    expect(r1.stdout).toMatch(new RegExp(`answered: .*${tid}`));
    const r2 = await runCli(['epic', 'review', gid], env, repo);
    expect(r2.stdout).toMatch(new RegExp(`reviewed: .*${tid}`));
    const status = await runCli(['status', tid], env, repo);
    expect(status.stdout).toMatch(/review/);
  }, 90_000);
});