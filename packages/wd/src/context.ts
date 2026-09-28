import { isEpic, type Ledger, type Work } from './ledger.ts';
import { run } from './runner/types.ts';

export type Workspace = {
  dir: string;
  repo: string | null;
  linked: boolean;
  branch: string | null;
  changed: string[];
};

/** Where is the executor standing: repo top, linked worktree path, current branch, dirty files. */
export async function workspaceContext(dir: string): Promise<Workspace> {
  const git = async (args: string[]): Promise<{ code: number; stdout: string }> => {
    const r = await run(['git', ...args], dir);
    return { code: r.code, stdout: r.stdout.trim() };
  };
  const top = await git(['rev-parse', '--show-toplevel', '--quiet']);
  if (top.code !== 0 || top.stdout === '') return { dir, repo: null, linked: false, branch: null, changed: [] };
  const list = await git(['worktree', 'list', '--porcelain']);
  const mainRoot = list.stdout.split('\n\n')[0]?.match(/^worktree\s+(.+)$/m)?.[1] ?? null;
  const branch = await git(['branch', '--show-current']);
  const short = (await git(['rev-parse', '--short', 'HEAD'])).stdout;
  const actual = branch.code === 0 && branch.stdout !== '' ? branch.stdout : short === '' ? null : `detached ${short}`;
  const changed = (await git(['status', '--porcelain'])).stdout.split('\n').filter((s) => s !== '').slice(0, 12);
  return { dir, repo: top.stdout, linked: top.stdout !== mainRoot && mainRoot !== null, branch: actual, changed };
}

export const contextLine = (w: Workspace): string => {
  if (w.repo === null) return `dir: ${w.dir} (no git repo)`;
  const where = w.linked ? ` · worktree: ${w.repo}` : '';
  return `dir: ${w.dir} · branch: ${w.branch ?? 'no commits'}${where} · ${w.changed.length} changed`;
};

/** The directory an executor works in: its registered cwd, else the epic's shared worktree, else "." */
export function effectiveCwd(w: Work, ledger: Ledger): string {
  if (w.cwd !== null) return w.cwd;
  const shared = isEpic(w.kind)
    ? ledger.worktrees(w.id).find((wt) => wt.kind === 'shared' && wt.state === 'active')
    : w.parent !== null ? ledger.worktrees(w.parent).find((wt) => wt.kind === 'shared' && wt.state === 'active') : undefined;
  return shared?.path ?? '.';
}