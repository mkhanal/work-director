import { join, resolve } from 'node:path';
import { isEpic, type Ledger, type Work } from './ledger.ts';
import type { Project } from './project.ts';

export type RunCli = (argv: string[]) => Promise<{ code: number; stdout: string }>;

/** Roll up a goal's children into a goal-level status: counts per state + done fraction. */
export function goalRollup(children: Work[]): { counts: Record<string, number>; done: number; total: number } {
  const counts: Record<string, number> = {};
  for (const c of children) counts[c.state] = (counts[c.state] ?? 0) + 1;
  return { counts, done: counts['done'] ?? 0, total: children.length };
}

/** Spawn the real `wd` CLI with the same env: every board action is a wrapper over the CLI. */
export function makeCliRunner(cliPath: string): RunCli {
  return async (argv) => {
    const p = Bun.spawn([process.execPath, cliPath, ...argv], { env: process.env, stdout: 'pipe', stderr: 'pipe', stdin: 'ignore' });
    const [so, se] = await Promise.all([new Response(p.stdout).text(), new Response(p.stderr).text()]);
    return { code: await p.exited, stdout: `${so}${se}`.trim() };
  };
}

export type UiOpts = {
  port: number;
  ledger: Ledger;
  projects: Map<string, Project>;
  cwd: string;
  runCli?: RunCli;
};

const esc = (s: string): string => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

const BADGE: Record<string, string> = {
  queued: '#6b7280', briefed: '#6b7280', running: '#f59e0b', 'needs-input': '#ef4444',
  review: '#3b82f6', 'soft-done': '#8b5cf6', done: '#22c55e', blocked: '#ef4444', dropped: '#6b7280',
};

export function startUi(o: UiOpts): { url: string; stop: () => void } {
  const runCli = o.runCli ?? makeCliRunner(join(resolve(import.meta.dir), 'cli.ts'));

  const goalView = (id: string) => {
    const epic = o.ledger.get(id);
    if (!isEpic(epic.kind)) throw new Error(`${id} is not an epic`);
    const children = o.ledger.tasks(id);
    return { epic, children, rollup: goalRollup(children), events: o.ledger.events(id) };
  };

  const board = () => {
    const all = o.ledger.list();
    const goals = all.filter((w) => isEpic(w.kind)).map((w) => ({ work: w, rollup: goalRollup(o.ledger.tasks(w.id)) }));
    const standalone = all.filter((w) => !isEpic(w.kind) && w.parent === null && w.state !== 'done' && w.state !== 'dropped');
    return { goals, standalone };
  };

  const server = Bun.serve({
    port: o.port,
    hostname: '127.0.0.1',
    async fetch(req) {
      const url = new URL(req.url);
      const path = url.pathname;
      if (req.method === 'POST' && path === '/api/action') {
        const body = (await req.json()) as { argv: string[] };
        const r = await runCli(body.argv);
        return Response.json(r);
      }
      if (req.method === 'GET' && path === '/api/goals') return Response.json(board());
      if (req.method === 'GET' && path.startsWith('/api/goal/')) {
        try { return Response.json(goalView(decodeURIComponent(path.slice('/api/goal/'.length)))); }
        catch (e) { return Response.json({ error: (e as Error).message }, { status: 404 }); }
      }
      if (path === '/api/board') return Response.json(board());
      if (path.startsWith('/api/events/')) {
        const id = decodeURIComponent(path.slice('/api/events/'.length));
        return Response.json(o.ledger.events(id).map((e) => ({ ...e, at: new Date(e.at).toLocaleString() })));
      }
      if (path === '/' || path === '/index.html') {
        return new Response(boardHtml(await board(), o.cwd, [...o.projects.keys()]), { headers: { 'content-type': 'text/html; charset=utf-8' } });
      }
      return new Response('not found', { status: 404 });
    },
  });
  const port = server.port;
  return { url: `http://127.0.0.1:${port}`, stop: () => server.stop(true) };
}

function boardHtml(b: { goals: { work: Work; rollup: { counts: Record<string, number>; done: number; total: number } }[]; standalone: Work[] }, cwd: string, projectNames: string[]): string {
  const goalCards = b.goals.map(({ work: w, rollup }) => {
    const counts = Object.entries(BADGE)
      .map(([st, color]) => ({ st, n: rollup.counts[st] ?? 0, color }))
      .filter((x) => x.n > 0)
      .map((x) => `<span class="dot" style="background:${x.color}"></span>${x.st} ${x.n}`)
      .join(' ');
    return `<div class="goal" data-id="${w.id}">
      <div class="goal-top"><strong>${esc(w.title)}</strong><span class="badge" style="background:${BADGE[w.state] ?? '#6b7280'}">${w.state}</span></div>
      <div class="goal-meta">${w.project} · ${w.id} · ${rollup.done}/${rollup.total} tasks done</div>
      <div class="goal-counts">${counts || 'no tasks yet'}</div>
      <div class="goal-actions">
        <button data-act='["epic","plan","${w.id}"]'>plan</button>
        <button data-act='["epic","run","${w.id}","--wait","--timeout","90"]'>run --wait</button>
        <button data-act='["epic","spawn","${w.id}"]'>spawn</button>
        <button data-act='["epic","status","${w.id}"]'>status</button>
      </div>
      <pre class="tasks hidden" id="tasks-${w.id}"></pre>
    </div>`;
  }).join('\n');
  const singles = b.standalone.map((w) => `<div class="single" data-id="${w.id}">
    <strong>${esc(w.title)}</strong><span class="badge" style="background:${BADGE[w.state] ?? '#6b7280'}">${w.state}</span>
    <span class="goal-meta">${w.project} · ${w.kind} · ${w.id}</span>
    <div class="goal-actions">
      <button data-act='["spawn","${w.id}"]'>spawn</button>
      <button data-act='["report","${w.id}"]'>report</button>
      <button data-act='["soft-done","${w.id}"]'>soft-done</button>
    </div></div>`
  ).join('\n');
  return `<!doctype html><html><head><meta charset="utf-8"><title>wd · goals board</title>
<style>
  :root { color-scheme: dark; }
  body { font: 14px/1.5 ui-monospace, SFMono-Regular, Menlo, monospace; background: #0b0f14; color: #d7e0ea; margin: 0; }
  header { padding: 14px 20px; border-bottom: 1px solid #1f2937; display: flex; justify-content: space-between; align-items: center; }
  main { padding: 16px 20px; max-width: 1080px; margin: 0 auto; }
  h1 { font-size: 18px; margin: 0; } h2 { font-size: 14px; text-transform: uppercase; letter-spacing: .08em; color: #8b98a8; }
  .goal, .single { background: #11171f; border: 1px solid #1f2937; border-radius: 8px; padding: 12px 14px; margin: 10px 0; }
  .goal-top { display: flex; justify-content: space-between; }
  .badge { color: #0b0f14; font-size: 11px; font-weight: 700; padding: 2px 8px; border-radius: 10px; }
  .goal-meta { color: #8b98a8; font-size: 12px; margin: 4px 0; }
  .goal-counts { font-size: 12px; margin: 4px 0; }
  .dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; margin: 0 4px 0 10px; }
  .goal-actions button { background: #1f2937; color: #d7e0ea; border: 0; border-radius: 6px; padding: 4px 10px; margin-right: 6px; font-size: 12px; cursor: pointer; }
  .goal-actions button:hover { background: #374151; }
  pre { background: #0b0f14; border: 1px solid #1f2937; border-radius: 6px; padding: 10px; overflow: auto; font-size: 12px; white-space: pre-wrap; }
  .hidden { display: none; }
  #toast { position: fixed; bottom: 16px; right: 16px; background: #1f2937; border: 1px solid #374151; padding: 10px 14px; border-radius: 8px; max-width: 60vw; display: none; }
  #create { display: flex; gap: 8px; margin: 12px 0; align-items: center; }
  #create input, #create select { background: #11171f; border: 1px solid #1f2937; color: #d7e0ea; padding: 6px 10px; border-radius: 6px; font: inherit; }
  #create input[type=text] { flex: 1; }
</style></head><body>
<header><h1>wd · goals board</h1><span>${esc(cwd)}</span></header>
<main>
  <h2>New goal (epic)</h2>
  <div id="create">
    <select id="project">${[...Array.from(new Set([...projectNames, ...b.goals.map((g) => g.work.project), ...b.standalone.map((s) => s.project)]))].map((p) => `<option>${esc(p)}</option>`).join('')}</select>
    <input type="text" id="title" placeholder="the goal…">
    <button onclick="create()">add epic</button>
  </div>
  <h2>Goals</h2>
  <div id="goals">${goalCards || '<p style="color:#8b98a8">no goals yet — add one above.</p>'}</div>
  ${singles ? `<h2>Standalone work</h2>${singles}` : ''}
</main>
<div id="toast"></div>
<script>
  const toast = (m) => { const t = document.getElementById('toast'); t.textContent = m; t.style.display = 'block'; setTimeout(() => t.style.display = 'none', 4000); };
  const act = async (argv) => { const r = await fetch('/api/action', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ argv }) }); return r.json(); };
  const create = async () => {
    const project = document.getElementById('project').value, title = document.getElementById('title').value.trim();
    if (!title) return;
    const { stdout, code } = await act(['add', project, title, '--kind', 'epic']);
    toast(stdout || 'exit ' + code); document.getElementById('title').value = ''; setTimeout(() => location.reload(), 600);
  };
  document.querySelectorAll('.goal-actions button').forEach((btn) => btn.addEventListener('click', async () => {
    const argv = JSON.parse(btn.dataset.act ?? '[]');
    btn.disabled = true;
    const { stdout, code } = await act(argv);
    btn.disabled = false;
    if (stdout) toast(stdout.split('\n').slice(-1)[0] || 'exit ' + code);
    setTimeout(() => location.reload(), 700);
  }));
  document.querySelectorAll('.goal').forEach((card) => card.querySelector('.goal-top strong')?.addEventListener('click', async () => {
    const id = card.dataset.id, pre = document.getElementById('tasks-' + id);
    pre.classList.toggle('hidden');
    if (pre.classList.contains('hidden')) return;
    const { stdout, code } = await act(['tasks', id]);
    pre.textContent = code !== 0 ? stdout : stdout || '(no open tasks)';
  }));
</script>
</body></html>`;
}