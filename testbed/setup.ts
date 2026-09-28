#!/usr/bin/env bun
import { join } from 'node:path';
import { bin, projectPath, run, setup, wdHome } from './lib.ts';

await setup();
console.log(`testbed ready

sample repo:  ${projectPath}   a fresh git repo, "bun test" passes
wd state:     ${wdHome}        empty ledger + project file
runners:      ${bin}           fake claude/opencode for offline runs

offline end-to-end:  bun testbed/eval.ts        drives the real wd CLI, no LLMs
drive it by hand:    set -a; source ${join(run, '.env-manual')}; set +a
                     wd status && wd add sample-app "<title>" && wd spawn <id>
reset anytime:       bun testbed/setup.ts        remakes run/, wipes all state`);