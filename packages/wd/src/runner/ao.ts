import { RunnerError, run, type Handle, type Runner, type SpawnOptions } from './types.ts';

// Agent Orchestrator (Untrivial-ai). Flags per `ao spawn --help` / `ao send --help` (installed build "dev", 2026-09-04).
const harness: Record<string, string> = { claude: 'claude-code', codex: 'codex', opencode: 'opencode' };
// `ao spawn` prints: spawned session <project>-<n> (idle) [prompt 58 B, system 7883 B]
const sessionIdFrom = (text: string): string | undefined => text.match(/spawned session (\S+)/)?.[1];

export const ao: Runner = {
  name: 'ao',
  async spawn(o: SpawnOptions): Promise<Handle> {
    const args = ['ao', 'spawn', '--name', o.name.slice(0, 20), '--prompt', o.brief];
    if (o.agent) args.push('--harness', harness[o.agent] ?? o.agent);
    if (process.env.AO_PROJECT_ID) args.push('--project', process.env.AO_PROJECT_ID);
    const r = await run(args, o.cwd);
    const session = r.code === 0 ? sessionIdFrom(r.stdout.trim()) : undefined;
    if (session === undefined) throw new RunnerError('ao', `spawn failed: ${r.stderr || r.stdout}`);
    return { runner: 'ao', session, ref: null, cwd: o.cwd };
  },
  async send(h, text) {
    const r = await run(['ao', 'send', '--session', h.session, '--message', text], h.cwd);
    if (r.code !== 0) throw new RunnerError('ao', `send failed: ${r.stderr || r.stdout}`);
  },
  async status(h) {
    const r = await run(['ao', 'session', 'get', h.session, '--json'], h.cwd);
    if (r.code !== 0) return 'unknown';
    // `ao session get --json` → { session: { activity: { state }, isTerminated, status } }
    const j = JSON.parse(r.stdout) as { session?: { activity?: { state?: string }; isTerminated?: boolean } };
    if (j.session?.isTerminated) return 'exited';
    const a = j.session?.activity?.state;
    return a === 'active' ? 'running' : a === 'idle' ? 'idle' : a === 'waiting_input' || a === 'blocked' ? 'waiting' : 'unknown';
  },
  async transcript() { return []; },
  attachHint: (h) => `Agent Orchestrator app → session ${h.session} (ao session get ${h.session} --json)`,
};
