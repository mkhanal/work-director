import { mkdir, readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { RunnerError, exe, run, waitFor, type Handle, type Runner, type SpawnOptions } from './types.ts';

export const logDir = (): string => join(process.env.WD_HOME ?? join(process.env.HOME ?? '', '.work-director'), 'codex');

const detach = async (args: string[], cwd: string, log: string): Promise<number> => {
  await mkdir(logDir(), { recursive: true });
  const [name, ...rest] = args;
  const p = Bun.spawn([exe(name ?? ''), ...rest], { cwd, stdout: Bun.file(log), stderr: Bun.file(`${log}.err`), stdin: 'ignore', env: process.env });
  p.unref();
  return p.pid;
};

const firstSessionId = async (log: string): Promise<string | undefined> => {
  const text = await readFile(log, 'utf8').catch(() => '');
  return text.match(/"session_id"\s*:\s*"([A-Za-z0-9-]{10,})"/)?.[1];
};

const alive = (pid: number): boolean => { try { process.kill(pid, 0); return true; } catch { return false; } };

const logs = new Map<string, string>();

export const codex: Runner = {
  name: 'codex',
  async spawn(o: SpawnOptions): Promise<Handle> {
    const log = join(logDir(), `${o.name.replace(/[^A-Za-z0-9-]+/g, '-')}-${Date.now()}.jsonl`);
    const args = ['codex', 'exec', '--cd', o.cwd, '--json', '--full-auto'];
    if (o.model) args.push('--model', o.model);
    const pid = await detach([...args, o.brief], o.cwd, log);
    const session = await waitFor(() => firstSessionId(log), 60_000);
    if (session === undefined) throw new RunnerError('codex', `no session_id in ${log}`);
    logs.set(session, log);
    return { runner: 'codex', session, ref: String(pid), cwd: o.cwd };
  },
  async send(h, text) {
    const log = join(logDir(), `${h.session}-${Date.now()}.jsonl`);
    const pid = await detach(['codex', 'exec', '--cd', h.cwd, '--json', 'resume', h.session, text], h.cwd, log);
    logs.set(h.session, log);
    h.ref = String(pid);
  },
  async status(h) {
    return h.ref !== null && alive(Number(h.ref)) ? 'running' : 'idle';
  },
  async transcript(h) {
    const log = logs.get(h.session);
    if (log === undefined) return [];
    const text = await readFile(log, 'utf8').catch(() => '');
    const out: string[] = [];
    for (const line of text.split('\n')) {
      if (line === '') continue;
      let evt: { thread?: { messages?: { role?: string; content?: { type?: string; text?: string }[] }[] }; rollup?: { summary?: string } };
      try { evt = JSON.parse(line) as typeof evt; } catch { continue; }
      for (const m of evt.thread?.messages ?? [])
        if (m.role === 'assistant') for (const p of m.content ?? []) if (p.type === 'text' && p.text) out.push(p.text);
      if (evt.rollup?.summary) out.push(evt.rollup.summary);
    }
    return out;
  },
  async models() {
    const r = await run(['codex', 'debug', 'models'], process.cwd());
    if (r.code !== 0) throw new RunnerError('codex', `models failed: ${r.stderr}`);
    return r.stdout.split('\n').map((s) => s.trim()).filter(Boolean);
  },
  attachHint: (h) => `cd ${h.cwd} && codex exec --json resume ${h.session}`,
};