import { mkdir, readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { RunnerError, exe, run, waitFor, type Handle, type Runner, type SpawnOptions } from './types.ts';

export const logDir = (): string => join(process.env.WD_HOME ?? join(process.env.HOME ?? '', '.work-director'), 'opencode');

const detach = async (args: string[], cwd: string, log: string): Promise<number> => {
  await mkdir(logDir(), { recursive: true });
  const [name, ...rest] = args;
  const p = Bun.spawn([exe(name ?? ''), ...rest], { cwd, stdout: Bun.file(log), stderr: Bun.file(`${log}.err`), stdin: 'ignore', env: process.env });
  p.unref();
  return p.pid;
};

const firstSessionId = async (log: string): Promise<string | undefined> => {
  const text = await readFile(log, 'utf8').catch(() => '');
  return text.match(/"sessionID":"(ses_[A-Za-z0-9]+)"/)?.[1];
};

const alive = (pid: number): boolean => { try { process.kill(pid, 0); return true; } catch { return false; } };

export const opencode: Runner = {
  name: 'opencode',
  async spawn(o: SpawnOptions): Promise<Handle> {
    const log = join(logDir(), `${o.name.replace(/[^A-Za-z0-9-]+/g, '-')}-${Date.now()}.jsonl`);
    const args = ['opencode', 'run', '--dir', o.cwd, '--format', 'json', '--title', o.name];
    if (o.agent) args.push('--agent', o.agent);
    const pid = await detach([...args, o.brief], o.cwd, log);
    const session = await waitFor(() => firstSessionId(log), 60_000);
    if (session === undefined) throw new RunnerError('opencode', `no sessionID in ${log}`);
    return { runner: 'opencode', session, ref: String(pid), cwd: o.cwd };
  },
  async send(h, text) {
    const log = join(logDir(), `${h.session}-${Date.now()}.jsonl`);
    const pid = await detach(['opencode', 'run', '--dir', h.cwd, '--format', 'json', '-s', h.session, text], h.cwd, log);
    h.ref = String(pid);
  },
  async status(h) {
    return h.ref !== null && alive(Number(h.ref)) ? 'running' : 'idle';
  },
  async transcript(h) {
    const r = await run(['opencode', 'export', h.session], h.cwd);
    if (r.code !== 0) throw new RunnerError('opencode', `export failed: ${r.stderr}`);
    const data = JSON.parse(r.stdout) as { messages?: { info?: { role?: string }; parts?: { type?: string; text?: string }[] }[] };
    const out: string[] = [];
    for (const m of data.messages ?? []) if (m.info?.role === 'assistant') for (const p of m.parts ?? []) if (p.type === 'text' && p.text) out.push(p.text);
    return out;
  },
  attachHint: (h) => `cd ${h.cwd} && opencode -s ${h.session}`,
};
