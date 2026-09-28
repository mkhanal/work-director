import { beforeAll, describe, expect, test } from 'bun:test';
import { chmod, mkdtemp, mkdir, readFile, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { isEpic, Ledger } from '../src/ledger.ts';
import { projectTemplate } from '../src/project.ts';

const fresh = (): Ledger => new Ledger(':memory:');

describe('A Goal Is A Distinct Work Kind', () => {
  test('goal add records kind goal; add --kind goal agrees; goals are epic-like', () => {
    const l = fresh();
    expect(isEpic('goal')).toBe(true);
    expect(isEpic('epic')).toBe(true);
    expect(isEpic('task')).toBe(false);
    const g = l.add('p', 'Ship the CLI', 'goal');
    expect(g.kind).toBe('goal');
    expect(l.add('p', 'same, via add', 'goal').kind).toBe('goal');
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

const setup = async (prefix: string): Promise<{ base: string; home: string; fakeState: string; repo: string; env: Record<string, string> }> => {
  const base = await mkdtemp(join(tmpdir(), prefix));
  const home = join(base, 'home', '.work-director');
  const fakeState = join(base, 'fake');
  const repo = join(base, 'repo');
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
  const env = { WD_HOME: home, WD_PROJECTS: join(home, 'projects'), HOME: home, WD_FAKE_STATE: fakeState };
  await writeFile(join(home, 'projects', 'wd.md'), projectTemplate('wd', repo, { runner: 'planner', mode: 'auto', verify: ['bun test'] }));
  return { base, home, fakeState, repo, env };
};

describe('A Goal Plan Decomposes It Into Headed Tasks', () => {
  test('goal add then goal plan files the planner tasks under the goal', async () => {
    const { home, repo, env } = await setup('wd-goal-plan-');
    const add = await runCli(['goal', 'add', 'wd', 'Bind the server', '--detail', 'the server must bind and answer'], env, repo);
    expect(add.stdout.trim()).toMatch(/^goal [0-9a-f]{8} queued — decompose it: wd goal plan [0-9a-f]{8}$/);
    const gid = add.stdout.trim().match(/^goal ([0-9a-f]{8}) queued/)?.[1];
    if (gid === undefined) throw new Error(`no goal id in:\n${add.stdout}`);
    const plan = await runCli(['goal', 'plan', gid], env, repo);
    expect(plan.stdout.trim().split('\n').map((x) => x.split('\t')).filter((x) => x[0]?.match(/^[0-9a-f]{8}$/))).toHaveLength(2);
    const status = await runCli(['goal', 'status', gid], env, repo);
    expect(status.stdout).toMatch(/briefed/);
    expect(status.stdout).toMatch(/2 open/);
  }, 90_000);
});

describe('A Goal Run Spawns Its Open Tasks On One Shared Worktree', () => {
  test('goal run starts every open task on the shared worktree', async () => {
    const { home, repo, env } = await setup('wd-goal-run-');
    const gid = (await runCli(['goal', 'add', 'wd', 'Bind the server'], env, repo)).stdout.trim().match(/^goal ([0-9a-f]{8}) queued/)?.[1];
    if (gid === undefined) throw new Error('no goal id');
    await runCli(['goal', 'plan', gid], env, repo);
    const run = await runCli(['goal', 'run', gid, '--runner', 'advisor', '--timeout', '3', '--wait'], env, repo);
    expect(run.stdout).toContain('open task'); // escalated to needs-input, not guessed
    const tasks = await runCli(['tasks', gid], env, repo);
    expect(tasks.stdout.trim().split('\n').filter((x) => x.match(/^-\s+[0-9a-f]{8}\s+(running|needs-input)/)), `tasks out:\n${tasks.stdout}`).toHaveLength(2);
  }, 90_000);
});

describe('A Goal Review Harvests Or Escalates Its Tasks', () => {
  test('a recorded decision answers the question; tasks move to review', async () => {
    const { repo, env } = await setup('wd-goal-review-');
    const gid = (await runCli(['goal', 'add', 'wd', 'Bind the server'], env, repo)).stdout.trim().match(/^goal ([0-9a-f]{8}) queued/)?.[1];
    if (gid === undefined) throw new Error('no goal id');
    await runCli(['goal', 'plan', gid], env, repo);
    const c = await runCli(['concern', 'add', gid, 'server port'], env, repo);
    const cid = c.stdout.match(/concern (\d+) on/)?.[1];
    if (cid === undefined) throw new Error(`no concern id in:\n${c.stdout}`);
    await runCli(['concern', 'resolve', cid, 'listen on port number 8080 bound to localhost'], env, repo);
    await runCli(['goal', 'run', gid, '--runner', 'advisor'], env, repo);
    const r1 = await runCli(['goal', 'review', gid], env, repo);
    expect(r1.stdout).toMatch(/answered:/);
    const r2 = await runCli(['goal', 'review', gid], env, repo);
    expect(r2.stdout).toMatch(/reviewed:/);
    const tasks = await runCli(['tasks', gid], env, repo);
    expect(tasks.stdout.trim().split('\n').filter((x) => x.match(/^-\s+[0-9a-f]{8}\s+review/)), `tasks out:\n${tasks.stdout}`).toHaveLength(2);
  }, 90_000);
});

describe('A Goal Status Reports Its Open Tasks', () => {
  test('status lists the goal state and each open task', async () => {
    const { repo, env } = await setup('wd-goal-status-');
    const gid = (await runCli(['goal', 'add', 'wd', 'Bind the server'], env, repo)).stdout.trim().match(/^goal ([0-9a-f]{8}) queued/)?.[1];
    if (gid === undefined) throw new Error('no goal id');
    await runCli(['goal', 'plan', gid], env, repo);
    const status = await runCli(['goal', 'status', gid], env, repo);
    expect(status.stdout).toMatch(/Bind the server/);
    expect(status.stdout.trim().split('\n').filter((x) => x.match(/^-\s+[0-9a-f]{8}\s+queued/))).toHaveLength(2);
  }, 90_000);
});