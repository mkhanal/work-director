import { readdir, readFile } from 'node:fs/promises';
import { basename, join } from 'node:path';
import { parseFrontmatter, list } from '../../taste/src/frontmatter.ts';
import { allRunnerNames } from './runner/registry.ts';

export const Runners = ['claude', 'opencode', 'codex'] as const;
// A runner is a built-in or any file of commands under ~/.work-director/runners/*.toml.
export type RunnerName = string;
export const Modes = ['ask', 'auto'] as const;
export type Mode = (typeof Modes)[number];

export type Project = {
  name: string; path: string; runner: RunnerName; agent: string | undefined; model: string | undefined; mode: Mode; stack: string[];
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
  const runner = need('runner');
  const mode = f.get('mode') ?? 'auto';
  if (runner.trim() === '') throw new ProjectError(path, 'missing field runner');
  if (!oneOf(Modes, mode)) throw new ProjectError(path, `unknown mode ${mode}`);
  return {
    name: basename(path, '.md'), path: need('path').replace(/^~/, process.env.HOME ?? '~'), runner, agent: f.get('agent'), model: f.get('model') || undefined, mode,
    stack: list(f.get('stack') ?? '[]'), workflows: list(f.get('workflows') ?? '[]'), verify: list(f.get('verify') ?? '[]'),
    instructionsFile: f.get('instructions_file') ?? 'AGENTS.md', defaultBranch: f.get('default_branch') ?? 'main', roadmap: fm.body,
  };
}

export type NewProjectOptions = {
  runner?: string | undefined; mode?: string | undefined; model?: string | undefined; stack?: string[] | undefined; workflows?: string[] | undefined; verify?: string[] | undefined;
  instructionsFile?: string | undefined; defaultBranch?: string | undefined; roadmap?: string | undefined;
};

/** The file `wd projects add` writes: concrete frontmatter, no lazyspec claim (presence lives in the repo it manages). */
export function projectTemplate(name: string, path: string, o: NewProjectOptions = {}): string {
  const listv = (v: string[] | undefined) => `[${(v ?? []).join(', ')}]`;
  return `---
path: ${path}
runner: ${o.runner ?? 'claude'}
mode: ${o.mode ?? 'auto'}
model: ${o.model ?? ''}
stack: ${listv(o.stack)}
workflows: ${listv(o.workflows)}
verify: ${listv(o.verify)}
instructions_file: ${o.instructionsFile ?? 'AGENTS.md'}
default_branch: ${o.defaultBranch ?? 'main'}
---
${o.roadmap ?? 'Roadmap, most important first. The director reads this file; it never edits the repo it describes.'}
`;
}

/** `--runner claude,opencode,myagent` parcels sessions across providers; empty list falls back. Unknown names fail loud here. */
export async function parseRunnerList(value: string, fallback: RunnerName): Promise<RunnerName[]> {
  const list = value.split(',').map((s) => s.trim()).filter(Boolean);
  const chosen = list.length > 0 ? list : [fallback];
  const known = new Set(await allRunnerNames());
  for (const r of chosen) if (!known.has(r)) throw new ProjectError('--runner', `unknown runner ${r}; known: ${[...known].join(', ')}`);
  return chosen;
}

/** The work item "yes" creates: install the director's preferred lazyspec in a project, as an evolution PR. */
export function installLazyspec(title: string): { title: string; detail: string } {
  return {
    title,
    detail: 'Install the director\'s preferred lazyspec in this repo: requirements live in `*.lazyspec.md`, each `## ` heading married to a test that repeats the heading word for word, changed together through the repo\'s own `/lazyspec`. Add the convention to the repo\'s agent files (instructions file, workflows), never to work-director files.',
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
