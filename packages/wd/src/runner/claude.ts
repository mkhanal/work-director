import { readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { RunnerError, run, waitFor, type Handle, type Runner, type SpawnOptions } from './types.ts';

type AgentRow = { id: string; sessionId: string; cwd: string; status?: string; state?: string };

const agents = async (cwd: string): Promise<AgentRow[]> => {
  const r = await run(['claude', 'agents', '--json', '--all'], cwd);
  return r.code === 0 ? (JSON.parse(r.stdout) as AgentRow[]) : [];
};

export const projectSlug = (cwd: string): string => cwd.replace(/[^A-Za-z0-9-]/g, '-');

export const claude: Runner = {
  name: 'claude',
  async spawn(o: SpawnOptions): Promise<Handle> {
    const args = ['claude', '--bg', '-n', o.name, '--permission-mode', o.permissionMode ?? 'bypassPermissions'];
    if (o.worktree) args.push('--worktree', o.name.toLowerCase().replace(/[^a-z0-9]+/g, '-'));
    args.push(o.brief);
    const r = await run(args, o.cwd);
    const ref = r.stdout.match(/backgrounded\s*·\s*([0-9a-f]{8})/)?.[1];
    if (r.code !== 0 || ref === undefined) throw new RunnerError('claude', `spawn failed: ${r.stderr || r.stdout}`);
    const row = await waitFor(async () => (await agents(o.cwd)).find((a) => a.id === ref), 15_000);
    if (row === undefined) throw new RunnerError('claude', `session ${ref} not listed by claude agents`);
    return { runner: 'claude', session: row.sessionId, ref, cwd: row.cwd };
  },
  async send(h, text) {
    const r = await run(['claude', '--bg', '--resume', h.session, text], h.cwd);
    if (r.code !== 0) throw new RunnerError('claude', `send failed: ${r.stderr || r.stdout}`);
  },
  async status(h) {
    const row = (await agents(h.cwd)).find((a) => a.sessionId === h.session);
    if (row === undefined) return 'exited';
    if (row.status === 'waiting') return 'waiting';
    if (row.status === 'busy' || row.status === 'running') return 'running';
    if (row.status === 'idle' || row.state === 'done') return 'idle';
    return 'unknown';
  },
  async transcript(h) {
    const file = join(process.env.HOME ?? '', '.claude', 'projects', projectSlug(h.cwd), `${h.session}.jsonl`);
    const text = await readFile(file, 'utf8').catch(() => '');
    const out: string[] = [];
    for (const line of text.split('\n')) {
      if (line === '') continue;
      const row = JSON.parse(line) as { type?: string; message?: { content?: unknown } };
      if (row.type !== 'assistant' || !Array.isArray(row.message?.content)) continue;
      for (const part of row.message.content as { type?: string; text?: string }[]) if (part.type === 'text' && part.text) out.push(part.text);
    }
    return out;
  },
  attachHint: (h) => `claude attach ${h.ref ?? h.session}`,
};
