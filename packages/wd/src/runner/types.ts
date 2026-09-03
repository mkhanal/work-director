import type { RunnerName } from '../project.ts';

export type Handle = { runner: RunnerName; session: string; ref: string | null; cwd: string };
export type SpawnOptions = { cwd: string; name: string; brief: string; agent?: string | undefined; permissionMode?: string | undefined; worktree?: boolean | undefined };
export type RunnerStatus = 'running' | 'idle' | 'waiting' | 'exited' | 'unknown';

export type Runner = {
  readonly name: RunnerName;
  spawn(o: SpawnOptions): Promise<Handle>;
  send(h: Handle, text: string): Promise<void>;
  status(h: Handle): Promise<RunnerStatus>;
  /** Assistant text of the session, oldest first. */
  transcript(h: Handle): Promise<string[]>;
  attachHint(h: Handle): string;
};

export class RunnerError extends Error {
  constructor(runner: RunnerName, detail: string) { super(`${runner}: ${detail}`); }
}

/** Resolve against the current PATH (Bun caches the startup PATH otherwise). */
export function exe(name: string): string {
  const found = Bun.which(name, { PATH: process.env.PATH ?? '' });
  if (found === null) throw new Error(`executable not found: ${name}`);
  return found;
}

export async function run(cmd: string[], cwd: string): Promise<{ code: number; stdout: string; stderr: string }> {
  const [name, ...args] = cmd;
  const p = Bun.spawn([exe(name ?? ''), ...args], { cwd, stdout: 'pipe', stderr: 'pipe', stdin: 'ignore', env: process.env });
  const [stdout, stderr, code] = await Promise.all([new Response(p.stdout).text(), new Response(p.stderr).text(), p.exited]);
  return { code, stdout, stderr };
}

export async function waitFor<T>(probe: () => Promise<T | undefined>, ms: number, every = 500): Promise<T | undefined> {
  const end = Date.now() + ms;
  while (Date.now() < end) {
    const v = await probe();
    if (v !== undefined) return v;
    await Bun.sleep(every);
  }
  return undefined;
}
