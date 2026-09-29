import { beforeAll, describe, expect, test } from 'bun:test';
import { mkdtemp, mkdir, writeFile, chmod, readFile, realpath } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { claude, projectSlug } from '../src/runner/claude.ts';
import { opencode } from '../src/runner/opencode.ts';
import { codex } from '../src/runner/codex.ts';
import { allRunnerNames, runnerNamed } from '../src/runner/registry.ts';

let bin = '', cwd = '', home = '';
const SID = 'b765c0a2-616a-4662-b7f3-c566a03f0db5';

// Fake CLIs record their argv to $bin/calls.log and answer like the real ones did on 2026-09-04.
beforeAll(async () => {
  bin = await mkdtemp(join(tmpdir(), 'wd-bin-'));
  home = await mkdtemp(join(tmpdir(), 'wd-home-'));
  cwd = await realpath(await mkdtemp(join(tmpdir(), 'wd-cwd-')));
  const fake = async (name: string, body: string) => { const p = join(bin, name); await writeFile(p, `#!/usr/bin/env bash\necho "${name} $*" >> "${bin}/calls.log"\n${body}`); await chmod(p, 0o755); };
  await fake('claude', `case "$1 $2" in
  "--bg -n") echo "backgrounded · b765c0a2 · $3";;
  "--bg --resume") echo "backgrounded · b765c0a2";;
  "agents --json") echo '[{"id":"b765c0a2","sessionId":"${SID}","cwd":"${cwd}","kind":"background","status":"idle","state":"done"}]';;
esac`);
  await fake('opencode', `case "$1" in
  run) echo '{"type":"step_start","sessionID":"ses_abc123"}'; echo '{"type":"text","sessionID":"ses_abc123","part":{"type":"text","text":"READY"}}';;
  session) echo '{"info":{"id":"ses_abc123"},"messages":[{"type":"user"},{"type":"assistant","content":[{"type":"text","text":"READY"}]},{"type":"assistant","content":[{"type":"text","text":"PONG"}]}]}';;
  models) echo 'anthropic/claude-opus-5'; echo 'openai/gpt-5.2';;
esac`);
  await fake('codex', `case "$1" in
  exec) echo '{"type":"thread","session_id":"codex-abc123","thread":{"messages":[]}}';;
  debug) echo 'codex/opus-5'; echo 'codex/sonnet-4-5';;
esac`);
  await fake('myagent', `case "$1" in
  run) echo "session=victory-001";;
  send) echo ok;;
  status) echo 'state: waiting';;
  export) echo 'row one'; echo 'row two';;
  models) echo 'victory/1';;
esac`);
  process.env.PATH = `${bin}:${process.env.PATH}`;
  process.env.HOME = home; process.env.WD_HOME = join(home, '.work-director');
  const dir = join(home, '.claude', 'projects', projectSlug(cwd));
  await mkdir(dir, { recursive: true });
  await writeFile(join(dir, `${SID}.jsonl`), [
    JSON.stringify({ type: 'user', message: { content: 'go' } }),
    JSON.stringify({ type: 'assistant', message: { content: [{ type: 'text', text: 'READY' }] } }),
    JSON.stringify({ type: 'assistant', message: { content: [{ type: 'text', text: 'STATUS: DONE' }] } }),
  ].join('\n'));
  const specDir = join(process.env.WD_HOME, 'runners');
  await mkdir(specDir, { recursive: true });
  await writeFile(join(specDir, 'myagent.toml'), `name = "myagent"
spawn = "myagent run --name {name} {brief}"
session_id = 'session=([A-Za-z0-9-]+)'
send = "myagent send {session} {text}"
status = "myagent status {session}"
running = 'state: *running'
waiting = 'state: *waiting'
exited = 'state: *exited'
transcript = "myagent export {session}"
models = "myagent models"
attach = "myagent attach {session}"
`);
});

const calls = async () => readFile(join(bin, 'calls.log'), 'utf8');
const list = async (h: { models?(): Promise<string[]> }): Promise<string[]> => (await h.models?.()) ?? [];

describe('Spawning Records Runner Session And Attach Hint', () => {
  test('claude: session id from claude agents, hint uses bg id', async () => {
    const h = await claude.spawn({ cwd, name: 'wd-1 t', brief: 'B' });
    expect(h).toEqual({ runner: 'claude', session: SID, ref: 'b765c0a2', cwd });
    expect(claude.attachHint(h)).toBe('claude attach b765c0a2');
    expect(await calls()).toContain('claude --bg -n wd-1 t --permission-mode bypassPermissions B');
  });
  test('opencode: session id from json stream, hint resumes it', async () => {
    const h = await opencode.spawn({ cwd, name: 'wd-2 t', brief: 'B' });
    expect(h.session).toBe('ses_abc123');
    expect(opencode.attachHint(h)).toBe(`cd ${cwd} && opencode -s ses_abc123`);
  });
});

describe('Sending Continues The Same Session', () => {
  test('claude resumes by session id without other flags', async () => {
    await claude.send({ runner: 'claude', session: SID, ref: 'b765c0a2', cwd }, 'more');
    expect(await calls()).toContain(`claude --bg --resume ${SID} more`);
  });
  test('opencode resumes with -s', async () => {
    const h = { runner: 'opencode' as const, session: 'ses_abc123', ref: null, cwd };
    await opencode.send(h, 'more');
    await Bun.sleep(200);
    expect(await calls()).toContain(`opencode run --format json --auto -s ses_abc123 more`);
  });
});

describe('A Transcript Yields The Executor Messages', () => {
  test('claude reads the jsonl under ~/.claude/projects/<slug>', async () => {
    expect(await claude.transcript({ runner: 'claude', session: SID, ref: 'b765c0a2', cwd })).toEqual(['READY', 'STATUS: DONE']);
  });
  test('opencode reads export', async () => {
    expect(await opencode.transcript({ runner: 'opencode', session: 'ses_abc123', ref: null, cwd })).toEqual(['READY', 'PONG']);
  });
});

describe('A Runner Forwards The Chosen Model', () => {
  test('claude passes --model on spawn', async () => {
    await claude.spawn({ cwd, name: 'wd-1 t', brief: 'B', model: 'fable' });
    expect(await calls()).toContain('claude --bg -n wd-1 t --permission-mode bypassPermissions --model fable B');
  });
  test('codex passes --model and records the session from its jsonl', async () => {
    const h = await codex.spawn({ cwd, name: 'wd-4 t', brief: 'B', model: 'opus' });
    expect(h.session).toBe('codex-abc123');
    expect(await calls()).toContain(`codex exec --cd ${cwd} --json --full-auto --model opus B`);
  });
  test('a spec runner whose spawn line has no {model} fails loud', async () => {
    const r = await runnerNamed('myagent');
    await expect(r.spawn({ cwd, name: 'wd-3 t', brief: 'B', model: 'fable' })).rejects.toThrow(/model/);
  });
});

describe('Model Lists Come From The Provider CLI, Not A Registry', () => {
  test('opencode and codex return whatever their own CLI prints', async () => {
    expect(await list(opencode)).toEqual(['anthropic/claude-opus-5', 'openai/gpt-5.2']);
    expect(await list(codex)).toEqual(['codex/opus-5', 'codex/sonnet-4-5']);
  });
  test('claude has no CLI list and returns none, never a guess', async () => {
    expect(await list(claude)).toEqual([]);
  });
});

describe('A Runner Can Be Defined By A File Of Commands', () => {
  test('a ~/.work-director/runners/*.toml drives spawn, send, status, transcript, models', async () => {
    expect((await allRunnerNames()).sort()).toEqual(['claude', 'codex', 'myagent', 'opencode']);
    const r = await runnerNamed('myagent');
    const h = await r.spawn({ cwd, name: 'wd-8 t', brief: 'B' });
    expect(h.session).toBe('victory-001');
    expect(r.attachHint(h)).toBe("myagent attach 'victory-001'");
    await r.send(h, 'more');
    const log = await calls();
    expect(log).toContain('myagent run --name wd-8 t B');
    expect(log).toContain(`myagent send victory-001 more`);
    expect(await r.status(h)).toBe('waiting');
    expect(await r.transcript(h)).toEqual(['row one', 'row two']);
    expect(await list(r)).toEqual(['victory/1']);
  });
  test('an unknown runner name fails loudly with guidance', async () => {
    await expect(runnerNamed('cursor')).rejects.toThrow(/cursor/);
  });
});
