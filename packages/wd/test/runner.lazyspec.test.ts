import { beforeAll, describe, expect, test } from 'bun:test';
import { mkdtemp, mkdir, writeFile, chmod, readFile, realpath } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { claude, projectSlug } from '../src/runner/claude.ts';
import { opencode } from '../src/runner/opencode.ts';
import { ao } from '../src/runner/ao.ts';

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
  export) echo '{"info":{"id":"ses_abc123"},"messages":[{"info":{"role":"user"},"parts":[{"type":"text","text":"hi"}]},{"info":{"role":"assistant"},"parts":[{"type":"text","text":"READY"}]},{"info":{"role":"assistant"},"parts":[{"type":"text","text":"PONG"}]}]}';;
esac`);
  await fake('ao', `case "$1" in
  spawn) echo "spawned session scratch-1 (idle) [prompt 58 B, system 7883 B]";;
  send) echo ok;;
  session) echo '{"session":{"id":"scratch-1","activity":{"state":"idle"},"isTerminated":false,"status":"idle"}}';;
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
});

const calls = async () => readFile(join(bin, 'calls.log'), 'utf8');

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
    expect(await calls()).toContain(`opencode run --dir ${cwd} --format json -s ses_abc123 more`);
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

describe('The Ao Adapter Spawns With The Brief As Prompt', () => {
  test('prompt, 20-char name, harness mapping, send flags, status', async () => {
    const h = await ao.spawn({ cwd, name: 'wd-3 a very long display name here', brief: 'B', agent: 'claude' });
    expect(h.session).toBe('scratch-1');
    const log = await calls();
    expect(log).toContain('ao spawn --name wd-3 a very long dis --prompt B --harness claude-code');
    await ao.send(h, 'more');
    expect(log + (await calls())).toContain(`ao send --session ${h.session} --message more`);
    expect(await ao.status(h)).toBe('idle');
  });
});
