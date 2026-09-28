import { beforeAll, describe, expect, test } from 'bun:test';
import { mkdtemp, realpath, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { contextLine, workspaceContext } from '../src/context.ts';
import { run } from '../src/runner/types.ts';

const git = async (args: string[], cwd: string) => run(['git', ...args], cwd);

describe('A Workspace Report Says Where The Executor Stands', () => {
  let plain = '', main = '', branch = '', linked = '', dirty = '';
  beforeAll(async () => {
    plain = await realpath(await mkdtemp(join(tmpdir(), 'wd-ctx-plain-')));
    main = await realpath(await mkdtemp(join(tmpdir(), 'wd-ctx-main-')));
    dirty = await realpath(await mkdtemp(join(tmpdir(), 'wd-ctx-dirty-')));
    for (const dir of [main, dirty]) {
      await git(['init', '-q'], dir);
      await git(['-c', 'user.name=wd', '-c', 'user.email=wd@wd', 'commit', '-q', '--allow-empty', '-m', 'init'], dir);
    }
    branch = await git(['branch', '--show-current'], main).then((r) => r.stdout.trim());
    linked = await realpath(await mkdtemp(join(tmpdir(), 'wd-ctx-linked-')));
    const add = await git(['worktree', 'add', '-b', 'wd-test', linked], main);
    if (add.code !== 0) throw new Error(add.stderr);
    await writeFile(join(dirty, 'scratch.txt'), 'dirty');
  });

  test('a directory outside any git repo reports no repo and no git claims', async () => {
    const w = await workspaceContext(plain);
    expect(w).toMatchObject({ repo: null, linked: false, branch: null, changed: [] });
    expect(contextLine(w)).toContain('no git repo');
  });

  test('a repo reports its top, current branch and changed files', async () => {
    const w = await workspaceContext(main);
    expect(w).toMatchObject({ repo: main, linked: false, branch });
    expect(contextLine(w)).toContain(`branch: ${branch}`);
    const d = await workspaceContext(dirty);
    expect(d.changed.join(' ')).toContain('scratch.txt');
    expect(contextLine(d)).toContain('1 changed');
  });

  test('a linked worktree is flagged so status can show where the repo sits', async () => {
    const w = await workspaceContext(linked);
    expect(w).toMatchObject({ repo: linked, linked: true, branch: 'wd-test' });
    expect(contextLine(w)).toContain('worktree:');
  });

  test('detached HEAD reads as detached <short sha>, not a branch name', async () => {
    await git(['checkout', '--detach', 'HEAD'], main);
    const w = await workspaceContext(main);
    expect(w.branch ?? '').toMatch(/^detached [0-9a-f]+$/);
    await git(['checkout', branch], main);
  });
});