import { Database } from 'bun:sqlite';

export const States = ['queued', 'briefed', 'running', 'needs-input', 'review', 'soft-done', 'done', 'blocked', 'dropped'] as const;
export type State = (typeof States)[number];
export const WorkKinds = ['task', 'evolution', 'workflow'] as const;
export type WorkKind = (typeof WorkKinds)[number];
export const EventKinds = ['state', 'report', 'verify', 'pr', 'note', 'sent'] as const;
export type EventKind = (typeof EventKinds)[number];
export const FeedbackSources = ['director', 'note', 'attached'] as const;
export type FeedbackSource = (typeof FeedbackSources)[number];

export type Work = {
  id: string; project: string; title: string; detail: string; kind: WorkKind; state: State;
  runner: string | null; session: string | null; ref: string | null; cwd: string | null; created: string; updated: string;
};
export type Event = { id: number; work: string; kind: EventKind; body: string; at: string };
export type Feedback = { id: number; text: string; project: string | null; card: string | null; source: FeedbackSource; at: string };
export type Candidate = { key: string; count: number; texts: string[] };

const transitions: Record<State, readonly State[]> = {
  queued: ['briefed', 'blocked', 'dropped'],
  briefed: ['running', 'queued', 'blocked', 'dropped'],
  running: ['needs-input', 'review', 'blocked', 'dropped'],
  'needs-input': ['running', 'blocked', 'dropped'],
  review: ['soft-done', 'running', 'blocked', 'dropped'],
  'soft-done': ['done', 'running', 'dropped'],
  blocked: ['queued', 'running', 'dropped'],
  done: [],
  dropped: [],
};

export class IllegalTransition extends Error {
  constructor(readonly from: State, readonly to: State) { super(`illegal transition ${from} → ${to}`); }
}
export class NotReady extends Error {
  constructor(readonly missing: string[]) { super(`not ready for soft-done: ${missing.join(', ')}`); }
}

const now = (): string => new Date().toISOString();
const newId = (): string => Math.random().toString(16).slice(2, 8);

export class Ledger {
  private readonly db: Database;

  constructor(path: string) {
    this.db = new Database(path, { create: true });
    this.db.exec(`
      PRAGMA journal_mode = WAL;
      CREATE TABLE IF NOT EXISTS work (id TEXT PRIMARY KEY, project TEXT NOT NULL, title TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '',
        kind TEXT NOT NULL, state TEXT NOT NULL, runner TEXT, session TEXT, ref TEXT, cwd TEXT, created TEXT NOT NULL, updated TEXT NOT NULL);
      CREATE TABLE IF NOT EXISTS event (id INTEGER PRIMARY KEY, work TEXT NOT NULL REFERENCES work(id), kind TEXT NOT NULL, body TEXT NOT NULL, at TEXT NOT NULL);
      CREATE TABLE IF NOT EXISTS feedback (id INTEGER PRIMARY KEY, text TEXT NOT NULL, project TEXT, card TEXT, source TEXT NOT NULL, at TEXT NOT NULL);
    `);
  }

  add(project: string, title: string, kind: WorkKind = 'task', detail = ''): Work {
    const id = newId(), t = now();
    this.db.run('INSERT INTO work (id, project, title, detail, kind, state, created, updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?)', [id, project, title, detail, kind, 'queued', t, t]);
    this.addEvent(id, 'state', 'queued');
    return this.get(id);
  }

  get(id: string): Work {
    const row = this.db.query<Work, [string]>('SELECT * FROM work WHERE id = ?').get(id);
    if (row === null) throw new Error(`no work ${id}`);
    return row;
  }

  list(filter: { project?: string; states?: readonly State[] } = {}): Work[] {
    const all = this.db.query<Work, []>('SELECT * FROM work ORDER BY created').all();
    return all.filter((w) => (filter.project === undefined || w.project === filter.project) && (filter.states === undefined || filter.states.includes(w.state)));
  }

  transition(id: string, to: State): Work {
    const w = this.get(id);
    if (!transitions[w.state].includes(to)) throw new IllegalTransition(w.state, to);
    this.db.run('UPDATE work SET state = ?, updated = ? WHERE id = ?', [to, now(), id]);
    this.addEvent(id, 'state', to);
    return this.get(id);
  }

  setSession(id: string, s: { runner: string; session: string; ref: string | null; cwd: string }): void {
    this.db.run('UPDATE work SET runner = ?, session = ?, ref = ?, cwd = ?, updated = ? WHERE id = ?', [s.runner, s.session, s.ref, s.cwd, now(), id]);
  }

  addEvent(work: string, kind: EventKind, body: string): void {
    this.db.run('INSERT INTO event (work, kind, body, at) VALUES (?, ?, ?, ?)', [work, kind, body, now()]);
  }

  events(work: string, kind?: EventKind): Event[] {
    const all = this.db.query<Event, [string]>('SELECT * FROM event WHERE work = ? ORDER BY id').all(work);
    return kind === undefined ? all : all.filter((e) => e.kind === kind);
  }

  /** review → soft-done, only with a DONE report, a passing verify, and a PR when code changed. */
  softDone(id: string, codeChanged: boolean): Work {
    const last = (k: EventKind): Event | undefined => this.events(id, k).at(-1);
    const missing: string[] = [];
    if (!last('report')?.body.startsWith('DONE')) missing.push('DONE report');
    if (!last('verify')?.body.startsWith('pass')) missing.push('passing verify');
    if (codeChanged && last('pr') === undefined) missing.push('pull request');
    if (missing.length > 0) throw new NotReady(missing);
    return this.transition(id, 'soft-done');
  }

  addFeedback(text: string, o: { project?: string; card?: string; source?: FeedbackSource } = {}): Feedback {
    const row = this.db.query<Feedback, [string, string | null, string | null, string, string]>('INSERT INTO feedback (text, project, card, source, at) VALUES (?, ?, ?, ?, ?) RETURNING *').get(text, o.project ?? null, o.card ?? null, o.source ?? 'director', now());
    if (row === null) throw new Error('feedback insert failed');
    return row;
  }

  feedback(): Feedback[] {
    return this.db.query<Feedback, []>('SELECT * FROM feedback ORDER BY id').all();
  }

  /** Feedback grouped by card (or project when no card) with two or more occurrences. */
  distill(): Candidate[] {
    const groups = new Map<string, string[]>();
    for (const f of this.feedback()) {
      const key = f.card !== null ? `card:${f.card}` : f.project !== null ? `project:${f.project}` : 'global';
      groups.set(key, [...(groups.get(key) ?? []), f.text]);
    }
    return [...groups].filter(([, t]) => t.length >= 2).map(([key, texts]) => ({ key, count: texts.length, texts }));
  }
}
