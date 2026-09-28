#!/usr/bin/env bun
// End-to-end eval: setup a throwaway testbed, then drive the real `wd` CLI through the whole
// epic flow (worktrees, claims, conflicts, concerns, merge gate, promotion scan) with fake
// runners on PATH, so every gate is exercised without real LLMs. Run: bun testbed/eval.ts
import { cli, env, setup } from './lib.ts';

await setup();

const out = (p: { stdout: ReadableStream }) => new Response(p.stdout).text();
const wd = async (args: string[], expectExit = 0) => {
  const p = Bun.spawn(['bun', cli, ...args], { env, stdout: 'pipe', stderr: 'pipe' });
  const [so, se] = await Promise.all([out(p), new Response(p.stderr).text()]);
  const code = await p.exited;
  if (code !== expectExit) throw new Error(`wd ${args.join(' ')} exited ${code} (want ${expectExit})\n${so}\n${se}`);
  return `${so}\n${se}`.trim();
};
let pass = 0, fail = 0;
const check = (name: string, cond: boolean, ctx = '') => {
  if (cond) { pass++; console.log(`  ok  ${name}`); }
  else { fail++; console.log(`FAIL  ${name}\n  ${ctx}`); }
};

console.log('epic + tasks grouped by heading');
const epicId = await wd(['add', 'sample-app', 'Metabase → Superset', '--kind', 'epic']);
const t1 = await wd(['add', 'sample-app', 'Port dashboards', '--epic', epicId, '--heading', 'Dashboards']);
const t2 = await wd(['add', 'sample-app', 'Rewrite ingestion', '--epic', epicId, '--heading', 'Data']);
const tasks = await wd(['tasks', epicId]);
check('tasks render grouped by heading', tasks.includes('## Data') && tasks.includes('## Dashboards'), tasks);
const tasksJson = JSON.parse(await wd(['tasks', epicId, '--json']));
check('tasks --json is the UI wrapper shape', Array.isArray(tasksJson) && tasksJson.every((t: { heading: string }) => 'heading' in t));

console.log('claims, impact, conflicts, concerns');
check('claim t1', (await wd(['claim', t1, 'ses_x'])).includes('ses_x'));
check('claim t2', (await wd(['claim', t2, 'ses_y'])).includes('ses_y'));
check('impact t1', (await wd(['impact', t1, '+src/dashboards'])).includes('src/dashboards'));
check('impact t2', (await wd(['impact', t2, '+src/ingest'])).includes('src/ingest'));
await wd(['impact', t2, '+src/dashboards']);
const conflict = await wd(['conflict', epicId]);
check('conflict t1/t2 on src/dashboards', conflict.includes(t1) && conflict.includes(t2) && conflict.includes('src/dashboards'), conflict);
await wd(['impact', t2, '-src/dashboards']);
check('conflict clears', !(await wd(['conflict', epicId])).includes('src/dashboards'));
const concernAdd = await wd(['concern', 'add', t2, 'metabase rate limits sync']);
const cid = concernAdd.match(/concern (\d+)/)?.[1] ?? '';
check('concern raised', cid !== '');
check('concern listed open', (await wd(['concern', 'list', epicId])).includes(t2));
check('concern resolves to decision', (await wd(['concern', 'resolve', cid, 'batch the sync'])).includes('batch the sync'));
check('concern list empty after resolve', !(await wd(['concern', 'list', epicId])).includes(t2));

console.log('task brief on a shared branch');
const brief = await wd(['brief', t1]);
check('brief shows co-worker claim and path', brief.includes('ses_y') && brief.includes('src/ingest'), brief);
check('slice brief has no roadmap', !brief.includes('validating the director') && brief.includes('wd conflict'));
check('brief names the repo workflows as the lazyspec signal', brief.includes('/lazyspec') && !brief.includes('Lazyspec applies'));

console.log('task t1: shared worktree, report, soft-done (no per-task verify/PR gate)');
const spawned = await wd(['spawn', t1]);
check('spawn on epic shared worktree', spawned.includes('running · claude'));
await wd(['events', epicId]);
check('report DONE -> review', (await wd(['report', t1])).includes('DONE'));
check('task soft-done with only DONE report', (await wd(['soft-done', t1])).includes('soft-done'));
await wd(['set', t1, 'done']);

console.log('task t2: on-demand private worktree, verify, merge gate');
const spawned2 = await wd(['spawn', t2, '--worktree']);
check('t2 spawned into a private worktree', spawned2.includes('running · claude'), spawned2);
check('merge refuses before verify', (await wd(['merge', t2], 1)).includes('refusing merge'));
check('report t2 DONE', (await wd(['report', t2])).includes('DONE'));
await wd(['verify', t2]);
check('conflict scan clean before merge', !(await wd(['conflict', epicId])).includes(t1));
const merged = await wd(['merge', t2]);
check('merge folds the private branch into the shared branch', merged.includes('merged') && merged.includes(epicId), merged);
check('worktree list shows private merged', (await wd(['worktree', 'list', t2])).includes('merged'));
const wts = await wd(['worktree', 'list', epicId]);
check('worktree list shows the epic shared worktree', wts.includes('shared') && wts.includes('active'), wts);
check('t2 soft-done after merge', (await wd(['soft-done', t2])).includes('soft-done'));
await wd(['set', t2, 'done']);

console.log('epic spawn (parallel sessions) + epic close');
const spawnOut = await wd(['epic', 'spawn', epicId, '--count', '2']);
check('two parallel sessions on one worktree', (spawnOut.match(/ses_/g) ?? []).length >= 2, spawnOut);
check('report epic DONE', (await wd(['report', epicId])).includes('DONE'));
await wd(['verify', epicId]);
await wd(['pr', epicId, 'https://github.com/x/sample-app/pull/1']);
check('epic soft-done: all tasks, verify, PR', (await wd(['soft-done', epicId])).includes('soft-done'));
check('epic done', (await wd(['done', epicId])).includes('done'));
check('tasks --all shows both done', (await wd(['tasks', epicId, '--all'])).includes('done') && (await wd(['tasks', epicId, '--all'])).includes(t2));

console.log('mixed-provider epic sessions');
const mixEpic = await wd(['add', 'sample-app', 'Mixed providers on one branch', '--kind', 'epic']);
const mixOut = await wd(['epic', 'spawn', mixEpic, '--count', '4', '--runner', 'claude,opencode,ao,codex']);
check('one spawn parses sessions across claude + opencode + ao + codex', mixOut.includes('ses_op') && mixOut.includes('ao-ses') && mixOut.includes('codex-s') && (mixOut.match(/ses_/g) ?? []).length >= 2, mixOut);
const modelsOut = await wd(['models', 'opencode']);
check('wd models lists models from the provider CLI, not a registry', modelsOut.includes('anthropic/claude-opus-5') && modelsOut.includes('openai/gpt-5.2'), modelsOut);
const allModelsOut = await wd(['models']);
check('wd models covers every runner and says so when a CLI has no list', allModelsOut.includes('codex/opus-5') && allModelsOut.includes('no CLI list'), allModelsOut);

console.log('runner-as-a-file (spec engine)');
const specTask = await wd(['add', 'sample-app', 'Vendor the SQL compiler', '--epic', mixEpic, '--heading', 'Data']);
const specSpawn = await wd(['spawn', specTask, '--runner', 'myagent']);
check('spawn drives a file-defined runner', specSpawn.includes('running · myagent · myagent-s') && specSpawn.includes('myagent attach'), specSpawn);
const runnerList = await wd(['runner', 'list']);
check('wd runner list includes the file-defined runner', runnerList.includes('myagent'), runnerList);
const specModels = await wd(['models', 'myagent']);
check('wd models reads a file-defined runner too', specModels.includes('victory/1'), specModels);

console.log('promotion scan');
await wd(['feedback', 'add', 'rule held up in sample-app', '--card', 'proj-rule', '--project', 'sample-app']);
await wd(['feedback', 'add', 'rule held up in other-app', '--card', 'proj-rule', '--project', 'other']);
await wd(['feedback', 'add', 'attached recurrence 1', '--card', 'proj-rule2', '--project', 'sample-app', '--source', 'attached']);
await wd(['feedback', 'add', 'attached recurrence 2', '--card', 'proj-rule2', '--project', 'sample-app', '--source', 'attached']);
const scan = await wd(['scan']);
check('scan lists promotion candidates', scan.includes('proj-rule') && scan.includes('proj-rule2'), scan);

console.log('project onboarding');
const withLs = await wd(['projects', 'add', 'new-app', '/tmp/x/new-app', '--lazyspec', 'y']);
const newId = withLs.match(/queued as ([0-9a-f]+) \(evolution/)?.[1] ?? '';
check('projects add writes the project file', (await wd(['projects'])).includes('new-app'));
check('--lazyspec y files an evolution install item', newId !== '' && withLs.includes('lazyspec'), withLs);
const withoutLs = await wd(['projects', 'add', 'plain-app', '/tmp/x/plain-app', '--lazyspec', 'n']);
check('--lazyspec n files no work item', !withoutLs.includes('(evolution — wd spawn') && withoutLs.includes('no lazyspec'), withoutLs);

console.log('epic plan + partial slice + attach (outside conversation driven by the coordinator)');
const planEpic = await wd(['add', 'sample-app', 'Plan and slice', '--kind', 'epic']);
const planOut = await wd(['epic', 'plan', planEpic, '--runner', 'planner']);
const planRows = planOut.split('\n').map((x) => x.split('\t')).filter((x) => x[0]?.match(/^[0-9a-f]{8}$/));
check('epic plan decomposes into headed tasks', planRows.length === 2 && planRows[0]?.[1] === 'Analysis', planOut);
const p1 = planRows[0]?.[0] ?? '', p2 = planRows[1]?.[0] ?? '';
const sliceOut = await wd(['epic', 'run', planEpic, '--only', p1, '--runner', 'advisor']);
check('run --only spawns just the one task', sliceOut.includes('1 task(s) running'), sliceOut);
const sliceTasks = await wd(['tasks', planEpic, '--all']);
check('the other task stays queued for another conversation', sliceTasks.includes(p2) && sliceTasks.includes('queued'), sliceTasks);
const attTask = await wd(['add', 'sample-app', 'Driver seat', '--epic', planEpic, '--heading', 'Analysis']);
const attOut = await wd(['attach', attTask, 'advisor-s9', '--runner', 'advisor']);
check('attach binds an outside session as the live session', attOut.includes('advisor:advisor-s9') && attOut.includes('(now running)'), attOut);
const attConcern = await wd(['concern', 'add', planEpic, 'server port']);
const attCid = attConcern.match(/concern (\d+)/)?.[1] ?? '';
await wd(['concern', 'resolve', attCid, 'listen on port number 8080 bound to localhost']);
const rev1 = await wd(['epic', 'review', planEpic]);
check('coordinator answers the attached conversation from the decision', rev1.includes('answered:') && rev1.includes(attTask), rev1);
const rev2 = await wd(['epic', 'review', planEpic]);
check('coordinator harvests the attached conversation, same LLM', rev2.includes('reviewed:') && rev2.includes(attTask), rev2);

console.log('goals board UI (wd ui, zero-dep Bun.serve, actions through the real CLI)');
const ui = Bun.spawn(['bun', cli, 'ui', '--port', '0'], { env, stdout: 'pipe', stderr: 'pipe' });
const uiReader = ui.stdout.getReader();
const uiDecoder = new TextDecoder();
let uiUrl = '', uiBuf = '';
const uiDeadline = Date.now() + 8000;
while (Date.now() < uiDeadline && uiUrl === '') {
  const { value, done } = await Promise.race([
    uiReader.read(),
    new Promise<{ value?: Uint8Array; done: boolean }>((r) => setTimeout(() => r({ done: false }), 200)),
  ]);
  if (done) break;
  if (value) uiBuf += uiDecoder.decode(value);
  uiUrl = uiBuf.match(/http:\/\/127\.0\.0\.1:\d+/)?.[0] ?? '';
}
ui.kill();
check('wd ui serves a goals board URL', uiUrl.startsWith('http://127.0.0.1:'), uiUrl || uiBuf);

const uiGoEpic = await wd(['add', 'sample-app', 'Board-worthy goal', '--kind', 'epic']);
const uiSrv = Bun.spawn(['bun', cli, 'ui', '--port', '0'], { env, stdout: 'pipe', stderr: 'pipe' });
const uiSrvReader = uiSrv.stdout.getReader();
let uiSrvUrl = '', uiSrvBuf = '';
const uiSrvDeadline = Date.now() + 8000;
while (Date.now() < uiSrvDeadline && uiSrvUrl === '') {
  const { value, done } = await Promise.race([
    uiSrvReader.read(),
    new Promise<{ value?: Uint8Array; done: boolean }>((r) => setTimeout(() => r({ done: false }), 200)),
  ]);
  if (done) break;
  if (value) uiSrvBuf += uiDecoder.decode(value);
  uiSrvUrl = uiSrvBuf.match(/http:\/\/127\.0\.0\.1:\d+/)?.[0] ?? '';
}
check('second board serves too (ephemeral ports)', uiSrvUrl.startsWith('http://127.0.0.1:'), uiSrvUrl || uiSrvBuf);
if (uiSrvUrl) {
  const board = (await (await fetch(`${uiSrvUrl}/api/goals`)).json()) as {
    goals: { work: { id: string; title: string }; rollup: { counts: Record<string, number>; done: number; total: number } }[];
    standalone: { id: string }[];
  };
  check('goals are first-class entries with rolled-up status', board.goals.some((g) => g.work.id === uiGoEpic) && typeof board.goals[0]?.rollup.total === 'number', JSON.stringify(board).slice(0, 300));
  const detail = (await (await fetch(`${uiSrvUrl}/api/goal/${uiGoEpic}`)).json()) as { epic: { id: string }; children: unknown[]; events: unknown[] };
  check('goal detail shows tasks and events', detail.epic.id === uiGoEpic && Array.isArray(detail.children) && Array.isArray(detail.events));
  const acted = (await (await fetch(`${uiSrvUrl}/api/action`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ argv: ['tasks', uiGoEpic] }),
  })).json()) as { code: number; stdout: string };
  check('board action runs through the real CLI', acted.code === 0 && acted.stdout.includes('- none'), acted.stdout.slice(0, 120));
}
uiSrv.kill();

console.log(`\neval: ${pass} passed, ${fail} failed`);
if (fail > 0) process.exit(1);