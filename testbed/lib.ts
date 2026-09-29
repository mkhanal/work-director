#!/usr/bin/env bun
import { chmod, copyFile, cp, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';

export const root = resolve(import.meta.dir, '.');
export const repoRoot = resolve(root, '..');
export const run = join(root, 'run');
export const home = join(run, 'home');
export const wdHome = join(run, 'wd-home');
export const projectsDir = join(wdHome, 'projects');
export const projectPath = join(run, 'sample-app');
export const bin = join(run, 'bin');
export const fakeState = join(run, 'fake-state');
export const cli = join(repoRoot, 'packages/wd/src/cli.ts');

/** Offline runs put the fake runners on PATH; hand-driven runs use a real runner (see .env-manual). */
export const env = {
  ...process.env,
  PATH: `${bin}:${process.env.PATH ?? ''}`,
  HOME: home,
  WD_HOME: wdHome,
  WD_PROJECTS: projectsDir,
  WD_FAKE_STATE: fakeState,
};

export async function git(args: string[], cwd = projectPath): Promise<void> {
  const p = Bun.spawn(['git', ...args], { cwd, env });
  if ((await p.exited) !== 0) throw new Error(`git ${args.join(' ')} failed`);
}

/** Remake testbed/run: a fresh sample-app repo, a seeded WD_HOME, fake runners on PATH. */
export async function setup(): Promise<void> {
  await rm(run, { recursive: true, force: true });
  for (const d of [home, fakeState, bin, projectsDir]) await mkdir(d, { recursive: true });

  await cp(join(root, 'sample-app'), projectPath, { recursive: true });
  await git(['init', '-b', 'main', '-q']);
  await git(['config', 'user.email', 'testbed@work-director']);
  await git(['config', 'user.name', 'Testbed']);
  await git(['add', '.']);
  await git(['commit', '-qm', 'init']);

  const template = await readFile(join(root, 'projects/sample-app.md'), 'utf8');
  await writeFile(join(projectsDir, 'sample-app.md'), template.replaceAll('TESTBED/', `${root}/`));

  for (const r of ['claude', 'opencode', 'codex', 'myagent', 'planner', 'advisor'] as const) {
    await copyFile(join(root, 'runners', r), join(bin, r));
    await chmod(join(bin, r), 0o755);
  }
  await cp(join(root, 'specs'), join(wdHome, 'runners'), { recursive: true });

  await writeFile(
    join(run, '.env-offline'),
    `export PATH=${bin}:$PATH\nexport HOME=${home}\nexport WD_HOME=${wdHome}\nexport WD_PROJECTS=${projectsDir}\nexport WD_FAKE_STATE=${fakeState}\n`,
  );
  await writeFile(
    join(run, '.env-manual'),
    `export WD_HOME=${wdHome}\nexport WD_PROJECTS=${projectsDir}\n# keep your real HOME/PATH: a real runner drives the sample repo\n`,
  );
}