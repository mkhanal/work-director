import { describe, expect, test } from 'bun:test';
import { Ledger } from '../src/ledger.ts';
import { startUi, goalRollup, type RunCli } from '../src/ui.ts';

const projects = new Map();

const luca = (): { ledger: Ledger; epic: string } => {
  const l = new Ledger(':memory:');
  const epic = l.add('p', 'Bind the server', 'epic');
  const open = l.add('p', 'parse config', 'task', '', { parent: epic.id });
  l.transition(open.id, 'briefed');
  l.transition(open.id, 'running');
  const done = l.add('p', 'wire schema', 'task', '', { parent: epic.id });
  for (const s of ['briefed', 'running', 'review'] as const) l.transition(done.id, s);
  l.addEvent(done.id, 'report', 'DONE\nwired');
  l.softDone(done.id, false);
  l.transition(done.id, 'done');
  l.addEvent(epic.id, 'note', 'plan: 2 tasks');
  l.add('p', 'fix the logo', 'task');
  return { ledger: l, epic: epic.id };
};

describe('The Board Lists Each Goal With A Rolled-Up Status', () => {
  test('/api/goals returns epics with per-state counts and done/total, standalone apart', async () => {
    const { ledger, epic } = luca();
    const srv = startUi({ port: 0, ledger, projects, cwd: '/tmp/x', runCli: async () => ({ code: 0, stdout: '' }) });
    try {
      const board = (await (await fetch(`${srv.url}/api/goals`)).json()) as {
        goals: { work: { id: string; title: string }; rollup: { counts: Record<string, number>; done: number; total: number } }[];
        standalone: { id: string; title: string }[];
      };
      expect(board.goals.map((g) => g.work.id)).toEqual([epic]);
      expect(board.goals[0]?.rollup).toEqual({ counts: { running: 1, done: 1 }, done: 1, total: 2 });
      expect(board.standalone.map((w) => w.title)).toEqual(['fix the logo']);
    } finally {
      srv.stop();
    }
  });
  test('goalRollup counts states and the done fraction', () => {
    const l = new Ledger(':memory:'), epic = l.add('p', 'e', 'epic');
    const a = l.add('p', 'a', 'task', '', { parent: epic.id });
    for (const s of ['briefed', 'running', 'review'] as const) l.transition(a.id, s);
    l.addEvent(a.id, 'report', 'DONE');
    l.softDone(a.id, false);
    l.transition(a.id, 'done');
    expect(goalRollup(l.tasks(epic.id))).toEqual({ counts: { done: 1 }, done: 1, total: 1 });
  });
});

describe('A Goal Detail Shows Tasks And Events', () => {
  test('/api/goal/<id> returns the epic, children, rollup and events', async () => {
    const { ledger, epic } = luca();
    const srv = startUi({ port: 0, ledger, projects, cwd: '/tmp/x', runCli: async () => ({ code: 0, stdout: '' }) });
    try {
      const detail = (await (await fetch(`${srv.url}/api/goal/${epic}`)).json()) as {
        epic: { id: string; state: string };
        children: { id: string }[];
        rollup: { counts: Record<string, number>; done: number };
        events: { kind: string }[];
      };
      expect(detail.epic.id).toBe(epic);
      expect(detail.children).toHaveLength(2);
      expect(detail.rollup.done).toBe(1);
      expect(detail.events.map((e) => e.kind)).toContain('note');
    } finally {
      srv.stop();
    }
  });
  test('an unknown or non-epic id is a 404, not an invented structure', async () => {
    const { ledger } = luca();
    const srv = startUi({ port: 0, ledger, projects, cwd: '/tmp/x', runCli: async () => ({ code: 0, stdout: '' }) });
    try {
      const bad = await fetch(`${srv.url}/api/goal/nope`);
      expect(bad.status).toBe(404);
      const standalone = ledger.list().find((w) => w.parent === null && !['epic'].includes(w.kind))!;
      const notEpic = await fetch(`${srv.url}/api/goal/${standalone.id}`);
      expect(notEpic.status).toBe(404);
    } finally {
      srv.stop();
    }
  });
});

describe('Every Board Action Runs Through The Real Cli', () => {
  test('POST /api/action invokes the injected runner and returns { code, stdout }', async () => {
    const { ledger } = luca();
    const calls: string[][] = [];
    const runCli: RunCli = async (argv) => { calls.push(argv); return { code: 0, stdout: 'done' }; };
    const srv = startUi({ port: 0, ledger, projects, cwd: '/tmp/x', runCli });
    try {
      const res = (await (await fetch(`${srv.url}/api/action`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ argv: ['add', 'p', 'a goal', '--kind', 'epic'] }),
      })).json()) as { code: number; stdout: string };
      expect(calls).toEqual([['add', 'p', 'a goal', '--kind', 'epic']]);
      expect(res).toEqual({ code: 0, stdout: 'done' });
    } finally {
      srv.stop();
    }
  });
});