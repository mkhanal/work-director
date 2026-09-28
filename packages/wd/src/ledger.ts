import { randomUUID } from 'node:crypto';
import { Database } from 'bun:sqlite';

export const States = ['queued', 'briefed', 'running', 'needs-input', 'review', 'soft-done', 'done', 'blocked', 'dropped'] as const;
export type State = (typeof States)[number];
export const WorkKinds = ['task', 'evolution', 'workflow', 'goal', 'epic'] as const;
export type WorkKind = (typeof WorkKinds)[number];
export const EventKinds = ['state', 'report', 'verify', 'pr', 'note', 'sent', 'spawn', 'attach', 'question', 'answer', 'decision'] as const;
export type EventKind = (typeof EventKinds)[number];
/** A goal is an epic-like: work broken into tasks under one branch. Goals stay a distinct kind
 *  so `wd status` and the UI can surface them as the entry points they are. */
export const isEpic = (kind: WorkKind): boolean => kind === 'goal' || kind === 'epic';
export const FeedbackSources = ['director', 'note', 'attached'] as const;
export type FeedbackSource = (typeof FeedbackSources)[number];
export const WorktreeKinds = ['shared', 'private'] as const;
export type WorktreeKind = (typeof WorktreeKinds)[number];
export const WorktreeStates = ['active', 'merged', 'abandoned'] as const;
export type WorktreeState = (typeof WorktreeStates)[number];

export type Work = {
  id: string; project: string; title: string; detail: string; kind: WorkKind; state: State;
  runner: string | null; session: string | null; ref: string | null; cwd: string | null; created: string; updated: string;
  parent: string | null; heading: string | null; claim: string | null; impact: string | null;
};
export type Event = { id: number; work: string; kind: EventKind; body: string; at: string };
export type Feedback = { id: number; text: string; project: string | null; card: string | null; source: FeedbackSource; at: string };
export type Candidate = { key: string; count: number; texts: string[] };
export type Concern = { id: number; work: string; text: string; resolved: number; decision: string | null; at: string; resolvedAt: string | null };
export type Worktree = { id: number; work: string; path: string; branch: string | null; kind: WorktreeKind; state: WorktreeState; created: string };
export type Conflict = { a: string; b: string; paths: string[] };

const transitions: Record<State, readonly State[]> = {
  queued: ['briefed', 'running', 'blocked', 'dropped'],
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
const newId = (): string => randomUUID().slice(0, 8);

export function splitImpact(impact: string | null): string[] {
  return (impact ?? '').split('\n').map((s) => s.trim()).filter((s) => s !== '');
}

/** Two paths clash when one is the other or one contains the other. */
export function pathsOverlap(a: string, b: string): boolean {
  const x = a.replace(/\/+$/, ''), y = b.replace(/\/+$/, '');
  return x === y || x.startsWith(`${y}/`) || y.startsWith(`${x}/`);
}

export type AddOptions = { kind?: WorkKind; detail?: string; parent?: string | null; heading?: string | null };

export class Ledger {
  private readonly db: Database;

  constructor(path: string) {
    this.db = new Database(path, { create: true });
    this.db.exec(`PRAGMA journal_mode = WAL;`);
    this.db.exec(`
      CREATE TABLE IF NOT EXISTS work (id TEXT PRIMARY KEY, project TEXT NOT NULL, title TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '',
        kind TEXT NOT NULL, state TEXT NOT NULL, runner TEXT, session TEXT, ref TEXT, cwd TEXT, created TEXT NOT NULL, updated TEXT NOT NULL,
        parent TEXT, heading TEXT, claim TEXT, impact TEXT);
      CREATE TABLE IF NOT EXISTS event (id INTEGER PRIMARY KEY, work TEXT NOT NULL REFERENCES work(id), kind TEXT NOT NULL, body TEXT NOT NULL, at TEXT NOT NULL);
      CREATE TABLE IF NOT EXISTS feedback (id INTEGER PRIMARY KEY, text TEXT NOT NULL, project TEXT, card TEXT, source TEXT NOT NULL, at TEXT NOT NULL);
      CREATE TABLE IF NOT EXISTS concern (id INTEGER PRIMARY KEY, work TEXT NOT NULL REFERENCES work(id), text TEXT NOT NULL, resolved INTEGER NOT NULL DEFAULT 0, decision TEXT, at TEXT NOT NULL, resolved_at TEXT);
      CREATE TABLE IF NOT EXISTS worktree (id INTEGER PRIMARY KEY, work TEXT NOT NULL REFERENCES work(id), path TEXT NOT NULL, branch TEXT, kind TEXT NOT NULL, state TEXT NOT NULL, created TEXT NOT NULL);
    `);
    // Additive migration for ledgers created before epics existed.
    const cols = this.db.query<{ name: string }, []>('PRAGMA table_info(work)').all().map((c) => c.name);
    for (const [col, decl] of [['parent', 'TEXT'], ['heading', 'TEXT'], ['claim', 'TEXT'], ['impact', 'TEXT']] as const) {
      if (!cols.includes(col)) this.db.run(`ALTER TABLE work ADD COLUMN ${col} ${decl}`);
    }
  }

  add(project: string, title: string, kind: WorkKind = 'task', detail = '', o: { parent?: string | null; heading?: string | null } = {}): Work {
    const parent = o.parent ?? null;
    if (parent !== null && isEpic(kind)) throw new Error('an epic cannot sit under another work item');
    if (parent !== null && !isEpic(this.get(parent).kind)) throw new Error(`parent ${parent} is not an epic`);
    const id = newId(), t = now();
    this.db.run('INSERT INTO work (id, project, title, detail, kind, state, parent, heading, created, updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)', [id, project, title, detail, kind, 'queued', parent, o.heading ?? null, t, t]);
    this.addEvent(id, 'state', 'queued');
    return this.get(id);
  }

get(id: string): Work {
    const w = this.db.query<Work, [string]>('SELECT * FROM work WHERE id = ?').get(id);
    if (w === undefined || w === null) {
      throw new Error(`no work ${id}; wd status for known work items`);
    }
    return w;
  }

  has(id: string): boolean {
    return this.db.query<{ n: number }, [string]>('SELECT COUNT(*) n FROM work WHERE id = ?').get(id)?.n === 1;
  }

  list(filter: { project?: string; states?: readonly State[] } = {}): Work[] {
    const all = this.db.query<Work, []>('SELECT * FROM work ORDER BY created').all();
    return all.filter((w) => (filter.project === undefined || w.project === filter.project) && (filter.states === undefined || filter.states.includes(w.state)));
  }

  /** Children of an epic, oldest first. */
  tasks(epicId: string): Work[] {
    return this.db.query<Work, [string]>('SELECT * FROM work WHERE parent = ? ORDER BY created').all(epicId);
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

  setCwd(id: string, cwd: string): void {
    this.db.run('UPDATE work SET cwd = ?, updated = ? WHERE id = ?', [cwd, now(), id]);
  }

  setClaim(id: string, claim: string | null): Work {
    this.db.run('UPDATE work SET claim = ?, updated = ? WHERE id = ?', [claim, now(), id]);
    return this.get(id);
  }

  setImpact(id: string, paths: string[]): Work {
    this.db.run('UPDATE work SET impact = ?, updated = ? WHERE id = ?', [paths.join('\n'), now(), id]);
    return this.get(id);
  }

  addEvent(work: string, kind: EventKind, body: string): void {
    this.db.run('INSERT INTO event (work, kind, body, at) VALUES (?, ?, ?, ?)', [work, kind, body, now()]);
  }

  events(work: string, kind?: EventKind): Event[] {
    const all = this.db.query<Event, [string]>('SELECT * FROM event WHERE work = ? ORDER BY id').all(work);
    return kind === undefined ? all : all.filter((e) => e.kind === kind);
  }

  addConcern(work: string, text: string): Concern {
    const row = this.db.query<Concern, [string, string, string]>('INSERT INTO concern (work, text, at) VALUES (?, ?, ?) RETURNING *').get(work, text, now());
    if (row === null) throw new Error('concern insert failed');
    return row;
  }

  resolveConcern(id: number, decision: string): Concern {
    const row = this.db.query<Concern, [string, string, number]>('UPDATE concern SET resolved = 1, decision = ?, resolved_at = ? WHERE id = ? RETURNING *').get(decision, now(), id);
    if (row === null) throw new Error(`no concern ${id}`);
    this.addEvent(row.work, 'note', `concern ${id} resolved: ${decision}`);
    return row;
  }

  concerns(work?: string): Concern[] {
    if (work === undefined) return this.db.query<Concern, []>('SELECT * FROM concern ORDER BY id').all();
    return this.db.query<Concern, [string]>('SELECT * FROM concern WHERE work = ? ORDER BY id').all(work);
  }

  openConcerns(epicId: string): Concern[] {
    const works = new Set([epicId, ...this.tasks(epicId).map((t) => t.id)]);
    return this.concerns().filter((c) => !c.resolved && works.has(c.work));
  }

  addWorktree(work: string, w: { path: string; branch: string | null; kind: WorktreeKind }): Worktree {
    const row = this.db.query<Worktree, [string, string, string | null, string, string, string]>('INSERT INTO worktree (work, path, branch, kind, state, created) VALUES (?, ?, ?, ?, ?, ?) RETURNING *').get(work, w.path, w.branch, w.kind, 'active', now());
    if (row === null) throw new Error('worktree insert failed');
    return row;
  }

  worktrees(work: string): Worktree[] {
    return this.db.query<Worktree, [string]>('SELECT * FROM worktree WHERE work = ? ORDER BY id').all(work);
  }

  setWorktreeState(id: number, state: WorktreeState): Worktree {
    const row = this.db.query<Worktree, [string, number]>('UPDATE worktree SET state = ? WHERE id = ? RETURNING *').get(state, id);
    if (row === null) throw new Error(`no worktree ${id}`);
    return row;
  }

  /** Concurrently claimed tasks of an epic whose impact paths overlap. */
  conflicts(epicId: string): Conflict[] {
    const tasks = this.tasks(epicId).filter((t) => t.claim !== null && t.state !== 'done' && t.state !== 'dropped');
    const out: Conflict[] = [];
    for (let i = 0; i < tasks.length; i++) {
      const a = tasks[i];
      if (a === undefined) continue;
      const pa = splitImpact(a.impact);
      for (let j = i + 1; j < tasks.length; j++) {
        const b = tasks[j];
        if (b === undefined) continue;
        const overlapping = pa.filter((x) => splitImpact(b.impact).some((y) => pathsOverlap(x, y)));
        if (overlapping.length > 0) out.push({ a: a.id, b: b.id, paths: overlapping });
      }
    }
    return out;
  }

  /** review → soft-done. Epic: needs every task done/dropped, DONE report, passing verify, PR. Task: needs only the DONE report. Standalone: DONE + verify, and PR when code changed. */
  softDone(id: string, codeChanged: boolean): Work {
    const w = this.get(id);
    const last = (k: EventKind): Event | undefined => this.events(id, k).at(-1);
    const missing: string[] = [];
    if (isEpic(w.kind)) {
      const open = this.tasks(id).filter((t) => t.state !== 'done' && t.state !== 'dropped');
      if (open.length > 0) missing.push(`${open.length} task(s) not done`);
      if (!last('report')?.body.startsWith('DONE')) missing.push('DONE report');
      if (!last('verify')?.body.startsWith('pass')) missing.push('passing verify');
      if (last('pr') === undefined) missing.push('pull request');
    } else if (w.parent !== null) {
      if (!last('report')?.body.startsWith('DONE')) missing.push('DONE report');
    } else {
      if (!last('report')?.body.startsWith('DONE')) missing.push('DONE report');
      if (!last('verify')?.body.startsWith('pass')) missing.push('passing verify');
      if (codeChanged && last('pr') === undefined) missing.push('pull request');
    }
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