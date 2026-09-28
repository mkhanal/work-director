import { ao } from './ao.ts';
import { claude } from './claude.ts';
import { codex } from './codex.ts';
import { opencode } from './opencode.ts';
import { loadSpecs, specDir, specRunner, type RunnerSpec } from './spec.ts';
import type { Runner } from './types.ts';

export { specDir, specTemplate, writeSpec } from './spec.ts';

// The four foundation adapters are code: claude's two-step session resolve,
// opencode/codex staged session discovery and their transcript stores needed
// logic. Every *other* provider is a file of commands (see `wd runner init`).
const builtin: Record<string, Runner> = { claude, opencode, codex, ao };
export const builtinNames = Object.keys(builtin);

let specsCache: { at: string; list: RunnerSpec[] } | undefined;
const specs = async (): Promise<RunnerSpec[]> => {
  const dir = specDir();
  if (specsCache === undefined || specsCache.at !== dir) {
    const list = await loadSpecs(dir);
    specsCache = { at: dir, list };
  }
  return specsCache.list;
};

export async function allRunnerNames(): Promise<string[]> {
  return [...builtinNames, ...(await specs()).map((s) => s.name)];
}

export async function runnerNamed(name: string): Promise<Runner> {
  const b = builtin[name];
  if (b !== undefined) return b;
  const s = (await specs()).find((x) => x.name === name);
  if (s === undefined) throw new Error(`no runner named "${name}"; known built-ins: ${builtinNames.join(', ')}. Add ~/.work-director/runners/${name}.toml (see wd runner init)`);
  return specRunner(s);
}