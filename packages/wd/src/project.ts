import { readdir, readFile } from 'node:fs/promises';
import { basename, join } from 'node:path';
import { parseFrontmatter, list } from '../../taste/src/frontmatter.ts';

export const Runners = ['claude', 'opencode', 'ao'] as const;
export type RunnerName = (typeof Runners)[number];
export const Modes = ['ask', 'auto'] as const;
export type Mode = (typeof Modes)[number];

export type Project = {
  name: string; path: string; runner: RunnerName; agent: string | undefined; mode: Mode; stack: string[];
  workflows: string[]; verify: string[]; instructionsFile: string; defaultBranch: string; roadmap: string;
};

export class ProjectError extends Error {
  constructor(readonly path: string, detail: string) { super(`${path}: ${detail}`); }
}

const oneOf = <T extends readonly string[]>(v: T, x: string): x is T[number] => v.includes(x);

export function parseProject(text: string, path: string): Project {
  const fm = parseFrontmatter(text);
  if (fm === undefined) throw new ProjectError(path, 'missing frontmatter');
  const f = fm.fields;
  const need = (k: string): string => {
    const v = f.get(k);
    if (v === undefined || v === '') throw new ProjectError(path, `missing field ${k}`);
    return v;
  };
  const runner = need('runner'), mode = f.get('mode') ?? 'ask';
  if (!oneOf(Runners, runner)) throw new ProjectError(path, `unknown runner ${runner}`);
  if (!oneOf(Modes, mode)) throw new ProjectError(path, `unknown mode ${mode}`);
  return {
    name: basename(path, '.md'), path: need('path').replace(/^~/, process.env.HOME ?? '~'), runner, agent: f.get('agent'), mode,
    stack: list(f.get('stack') ?? '[]'), workflows: list(f.get('workflows') ?? '[]'), verify: list(f.get('verify') ?? '[]'),
    instructionsFile: f.get('instructions_file') ?? 'AGENTS.md', defaultBranch: f.get('default_branch') ?? 'main', roadmap: fm.body,
  };
}

export async function loadProjects(dir: string): Promise<Map<string, Project>> {
  const out = new Map<string, Project>();
  for (const f of await readdir(dir)) {
    if (!f.endsWith('.md') || f === 'README.md') continue;
    const p = parseProject(await readFile(join(dir, f), 'utf8'), join(dir, f));
    out.set(p.name, p);
  }
  return out;
}
