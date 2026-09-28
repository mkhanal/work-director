import { afterAll, beforeAll, describe, expect, test } from 'bun:test';
import { mkdtemp, writeFile, chmod } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { detectEditor, openLink, parseTarget } from '../src/open.ts';

describe('An Open Target Resolves Against The Cwd', () => {
  test('relative <path>:<line> resolves absolutely and carries the line', () => {
    const t = parseTarget('src/app.ts:21', '/work/shop');
    expect(t).toEqual({ path: '/work/shop/src/app.ts', line: 21 });
  });
  test('a bare path resolves with no line', () => {
    expect(parseTarget('notes.md', '/work/shop')).toEqual({ path: '/work/shop/notes.md', line: null });
  });
  test('an absolute path passes through untouched', () => {
    expect(parseTarget('/tmp/x/y.ts:3', '/work/shop')).toEqual({ path: '/tmp/x/y.ts', line: 3 });
  });
});

describe('The Host Editor Is Detected From What Is Installed', () => {
  let vscodeBin = '', emptyBin = '';
  beforeAll(async () => {
    vscodeBin = await mkdtemp(join(tmpdir(), 'wd-vscode-'));
    emptyBin = await mkdtemp(join(tmpdir(), 'wd-empty-'));
    const code = join(vscodeBin, 'code');
    await writeFile(code, '#!/usr/bin/env bash\nexit 0\n');
    await chmod(code, 0o755);
  });

  test('VS Code CLI wins when code is on PATH, reusing the open window and jumping to the line', async () => {
    const env = { PATH: `${vscodeBin}:${process.env.PATH}`, VISUAL: undefined, EDITOR: undefined };
    const e = await detectEditor(env);
    expect(e?.label).toBe('Visual Studio Code');
    expect(e?.args({ path: '/a/b.ts', line: 9 })).toEqual(['--reuse-window', '--goto', '/a/b.ts:9']);
    expect(e?.args({ path: '/a/b.ts', line: null })).toEqual(['--reuse-window', '/a/b.ts']);
  });
  test('$VISUAL/$EDITOR become a plain open command with no code binary, never a fabricated editor', async () => {
    let e = await detectEditor({ PATH: emptyBin, VISUAL: 'mate -w', EDITOR: undefined });
    expect(e?.label).toBe('mate -w');
    expect(e?.args({ path: '/a/b.ts', line: null })).toEqual(['-w', '/a/b.ts']);
    e = await detectEditor({ PATH: emptyBin, VISUAL: undefined, EDITOR: 'vim' });
    expect(e?.args({ path: '/a/b.ts', line: 3 })).toEqual(['/a/b.ts']);
    expect(await detectEditor({ PATH: emptyBin, VISUAL: undefined, EDITOR: undefined })).toBeNull();
  });
});

describe('The File Link Is A Clickable OsC8 Url', () => {
  const oldHost = process.env.HOSTNAME;
  beforeAll(() => { delete process.env.HOSTNAME; });
  afterAll(() => { if (oldHost) process.env.HOSTNAME = oldHost; });

  test('renders a file hyperlink with the absolute path and line', () => {
    const link = openLink({ path: '/work/shop/src/app.ts', line: 21 });
    expect(link).toContain('\x1b]8;;file://localhost/work/shop/src/app.ts#21\x1b\\');
    expect(link.endsWith('\x1b]8;;\x1b\\')).toBe(true);
  });
  test('a path without a line still links, labelled by the path alone', () => {
    expect(openLink({ path: '/work/shop/notes.md', line: null })).toContain('file://localhost/work/shop/notes.md\x1b\\/work/shop/notes.md\x1b]8;;\x1b\\');
  });
});