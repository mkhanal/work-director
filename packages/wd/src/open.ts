import { basename, dirname, resolve } from 'node:path';
import { run } from './runner/types.ts';

export type OpenTarget = { path: string; line: number | null };

/** `<path>:<line>` → absolute path + line; no colon match → plain path. */
export function parseTarget(file: string, baseDir: string): OpenTarget {
  const m = file.match(/^(.*[^:]):(\d+)$/);
  return m === null ? { path: resolve(baseDir, file), line: null } : { path: resolve(baseDir, m[1]!), line: Number(m[2]) };
}

export type Editor = { label: string; cmd: string; args: (target: OpenTarget) => string[] };

/** The terminal host's editor, in order of preference: VS Code (reuse-window), then $VISUAL/$EDITOR. */
export async function detectEditor(env: Record<string, string | undefined> = process.env): Promise<Editor | null> {
  if (Bun.which('code', env.PATH ? { PATH: env.PATH } : undefined)) {
    return { label: 'Visual Studio Code', cmd: 'code', args: (t) => (t.line === null ? ['--reuse-window', t.path] : ['--reuse-window', '--goto', `${t.path}:${t.line}`]) };
  }
  const v = env.VISUAL ?? env.EDITOR;
  if (v !== undefined && v !== '') {
    const [cmd, ...rest] = v.split(/\s+/);
    return { label: v, cmd: cmd!, args: (t) => [...rest, t.path] };
  }
  return null;
}

/** iTerm2/kitty-style clickable file link (OSC-8), so a path printed in wd output can be cmd-clicked. */
export function openLink(target: OpenTarget): string {
  const host = process.env.HOSTNAME ?? 'localhost';
  const label = target.line === null ? target.path : `${target.path}:${target.line}`;
  return `\x1b]8;;file://${host}${target.path}${target.line === null ? '' : `#${target.line}`}\x1b\\${label}\x1b]8;;\x1b\\`;
}

export async function openInEditor(editor: Editor, target: OpenTarget): Promise<boolean> {
  const r = await run([editor.cmd, ...editor.args(target)], dirname(target.path) || '/');
  return r.code === 0;
}

export const fileLabel = (target: OpenTarget): string => `${basename(target.path)}${target.line === null ? '' : `:${target.line}`}`;