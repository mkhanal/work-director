import type { RunnerName } from '../project.ts';
import { ao } from './ao.ts';
import { claude } from './claude.ts';
import { opencode } from './opencode.ts';
import type { Runner } from './types.ts';

const runners: Record<RunnerName, Runner> = { claude, opencode, ao };
export const runnerNamed = (name: RunnerName): Runner => runners[name];
