import { mkdir, readFile, readdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { RunnerError, run, waitFor, type Handle, type Runner, type RunResult, type SpawnOptions } from './types.ts';

export const specDir = (): string => join(process.env.WD_HOME ?? join(process.env.HOME ?? '', '.work-director'), 'runners');

export type RunnerSpec = {
  name: string;
  spawn?: string;
  detach?: boolean;
  session_id?: string;
  send?: string;
  status?: string;
  running?: string;
  waiting?: string;
  exited?: string;
  transcript?: string;
  models?: string;
  attach?: string;
};

export const loadSpecs = async (dir: string = specDir()): Promise<RunnerSpec[]> => {
  const files = await readdir(dir).catch(() => []);
  const out: RunnerSpec[] = [];
  for (const f of files) {
    if (!f.endsWith('.toml')) continue;
    const parsed = Bun.TOML.parse(await readFile(join(dir, f), 'utf8')) as RunnerSpec;
    out.push({ ...parsed, name: parsed.name ?? f.replace(/\.toml$/, '') });
  }
  return out;
};

export const writeSpec = async (name: string, text: string): Promise<string> => {
  const spec = Bun.TOML.parse(text) as RunnerSpec;
  if (!spec.spawn) throw new RunnerError(name, 'spawn command is required');
  if (!spec.session_id) throw new RunnerError(name, 'session_id regex is required (capture the session id in spawn output)');
  await mkdir(specDir(), { recursive: true });
  const file = join(specDir(), `${name}.toml`);
  await writeFile(file, text.replace(/^name\s*=.*$/m, `name = "${name}"`));
  return file;
};

export const specTemplate = (name: string): string => `# ${name} — a runner is one file of commands against a provider's CLI.
# Placeholders are shell-quoted automatically: {cwd} {name} {brief} {model} {agent}
# {session} {text} {home} {slug_cwd} {log} {name20}. Write commands exactly as you
# would run them, including pipes and redirects.
name = "${name}"
spawn = "${name} run --cwd {cwd} --title {name} --json {brief}"
detach = true
session_id = '"session":"([A-Za-z0-9_-]+)"'
send = "${name} send --session {session} {text}"
running = 'state: *running'
waiting = 'state: *waiting'
exited = 'state: *exited'
transcript = "${name} export --session {session}"
models = "${name} models"
attach = "cd {cwd} && ${name} -s {session}"

# status: omit to report liveness only; include to classify output by these regexes
# (exited, then waiting, then running). transcript/models return one item per line.
# session_id is required: the first match in spawn output names the session.
`;

const q = (s: string): string => `'${s.replace(/'/g, '\'\\\'\'')}'`;
const fill = (tpl: string, v: Record<string, string>): string =>
  tpl.replace(/\{(\w+)\}/g, (_p, k: string) => (k in v ? q(v[k] ?? '') : ''));

const firstId = (text: string, re: string): string | undefined => {
  const m = new RegExp(re).exec(text);
  return m?.[1] ?? m?.[0];
};

const shell = (cmd: string, cwd: string): Promise<RunResult> => run(['bash', '-lc', cmd], cwd);

export const specRunner = (spec: RunnerSpec): Runner => {
  const lines = (text: string): string[] => text.split('\n').map((s) => s.trim()).filter(Boolean);
  return {
    name: spec.name,
    async spawn(o: SpawnOptions): Promise<Handle> {
      if (!spec.spawn || !spec.session_id) throw new RunnerError(spec.name, 'spec needs spawn and session_id');
      const sessionRegex = spec.session_id;
      const common = {
        cwd: o.cwd, name: o.name, name20: o.name.slice(0, 20), brief: o.brief,
        model: o.model ?? '', agent: o.agent ?? '', home: process.env.HOME ?? '',
        slug_cwd: o.cwd.replace(/[^A-Za-z0-9-]/g, '-'), session: '', text: '', log: '',
      };
      const cmd = fill(spec.spawn, common);
      if (spec.detach) {
        await mkdir(specDir(), { recursive: true });
        const log = join(specDir(), `${spec.name}-${Date.now()}.log`);
        const p = Bun.spawn(['bash', '-lc', cmd], { cwd: o.cwd, stdout: Bun.file(log), stderr: Bun.file(`${log}.err`), stdin: 'ignore', env: process.env });
        p.unref();
        const session = await waitFor(async () => firstId(await readFile(log, 'utf8').catch(() => ''), sessionRegex), 60_000);
        if (session === undefined) throw new RunnerError(spec.name, `no session id in ${log}`);
        return { runner: spec.name, session, ref: String(p.pid), cwd: o.cwd };
      }
      const r = await shell(cmd, o.cwd);
      const session = firstId(r.stdout + r.stderr, sessionRegex);
      if (session === undefined) throw new RunnerError(spec.name, `no session id in spawn output: ${r.stderr || r.stdout}`);
      return { runner: spec.name, session, ref: null, cwd: o.cwd };
    },
    async send(h, text) {
      if (!spec.send) throw new RunnerError(spec.name, 'no send command in spec');
      const r = await shell(fill(spec.send, { cwd: h.cwd, name: '', name20: '', brief: '', model: '', agent: '', home: process.env.HOME ?? '', slug_cwd: '', session: h.session, text, log: '' }), h.cwd);
      if (r.code !== 0) throw new RunnerError(spec.name, `send failed: ${r.stderr}`);
    },
    async status(h) {
      if (spec.status) {
        const r = await shell(fill(spec.status, { cwd: h.cwd, name: '', name20: '', brief: '', model: '', agent: '', home: process.env.HOME ?? '', slug_cwd: '', session: h.session, text: '', log: '' }), h.cwd);
        if (r.code !== 0) return 'unknown';
        const body = r.stdout;
        if (spec.exited && new RegExp(spec.exited).test(body)) return 'exited';
        if (spec.waiting && new RegExp(spec.waiting).test(body)) return 'waiting';
        if (spec.running && new RegExp(spec.running).test(body)) return 'running';
        return 'idle';
      }
      if (h.ref !== null) { try { process.kill(Number(h.ref), 0); return 'running'; } catch { return 'idle'; } }
      return 'idle';
    },
    async transcript(h) {
      if (!spec.transcript) return [];
      const r = await shell(fill(spec.transcript, { cwd: h.cwd, name: '', name20: '', brief: '', model: '', agent: '', home: process.env.HOME ?? '', slug_cwd: '', session: h.session, text: '', log: '' }), h.cwd);
      if (r.code !== 0) throw new RunnerError(spec.name, `transcript failed: ${r.stderr}`);
      return lines(r.stdout);
    },
    async models() {
      if (!spec.models) return [];
      const r = await shell(fill(spec.models, { cwd: process.cwd(), name: '', name20: '', brief: '', model: '', agent: '', home: process.env.HOME ?? '', slug_cwd: '', session: '', text: '', log: '' }), process.cwd());
      if (r.code !== 0) throw new RunnerError(spec.name, `models failed: ${r.stderr}`);
      return lines(r.stdout);
    },
    attachHint: (h) => (spec.attach ? fill(spec.attach, { cwd: h.cwd, name: '', name20: '', brief: '', model: '', agent: '', home: process.env.HOME ?? '', slug_cwd: '', session: h.session, text: '', log: '' }) : `${spec.name} session ${h.session}`),
  };
};