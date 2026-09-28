import type { Project } from './project.ts';
import { type Ledger, type Work } from './ledger.ts';
import { runnerNamed } from './runner/registry.ts';
import type { Runner } from './runner/types.ts';
import { waitFor } from './runner/types.ts';

export const epicPlanBrief = (epic: Work, project: Project): string => `You are the director's planning model for the project ${project.name} (${project.path}).

Analyse this goal and decompose it into concrete, independent tasks. Read the repo to ground yourself, but do NOT edit, create or open any files; analysis only. Reply with ONLY the task list in exactly this shape — one line per task, grouped under a heading:

## <heading>
- [ ] <task title>
- [ ] <task title>

Cover what must change and be verified; keep tasks small enough that one session can finish each. Project verify commands: ${project.verify.length > 0 ? project.verify.map((v) => `\`${v}\``).join(', ') : 'none listed'}. The goal: ${epic.title}${epic.detail ? `\n${epic.detail}` : ''}`;

export type PlanTask = { heading: string; title: string };

/** Parse the planner session's reply into tasks. Heading lines open a group; `- [ ]` lines are tasks. */
export function parsePlan(texts: string[]): PlanTask[] {
  let heading = '(no heading)';
  const out: PlanTask[] = [];
  for (const line of texts.flatMap((t) => t.split('\n'))) {
    const h = line.match(/^##\s+(.+)/);
    if (h !== null) { heading = h[1]!.trim(); continue; }
    const t = line.match(/^[-*]\s+\[[ xX]\]\s*(.+)/);
    if (t !== null) out.push({ heading, title: t[1]!.trim() });
  }
  return out;
}

/** A known answer: a resolved concern's decision or a `decision` event on the epic or its children
 *  that shares ≥2 significant words with the question (exact containment wins). */
export function knownAnswer(question: string, epic: Work, children: Work[], ledger: Ledger): string | undefined {
  const candidates: string[] = [];
  for (const w of [epic, ...children]) {
    for (const c of ledger.concerns(w.id)) if (c.resolved === 1 && c.decision !== null) candidates.push(c.decision);
    for (const e of ledger.events(w.id)) if (e.kind === 'decision') candidates.push(e.body);
  }
  const lower = question.toLowerCase();
  const exact = candidates.find((c) => c.toLowerCase().includes(lower));
  if (exact !== undefined) return exact;
  const q = tokens(lower);
  const overlap = candidates.map((c) => ({ c, n: countShared(tokens(c.toLowerCase()), q) })).filter((x) => x.n >= 2);
  return overlap.sort((a, b) => b.n - a.n)[0]?.c;
}

const tokens = (s: string): Set<string> =>
  new Set(s.replace(/[^a-z0-9\s-]/g, ' ').split(/\s+/).filter((t) => t.length >= 4));
const countShared = (a: Set<string>, b: Set<string>): number => { let n = 0; for (const t of a) if (b.has(t)) n++; return n; };

export type PassResult = { answered: string[]; escalated: string[]; reviewed: string[]; blocked: string[]; waiting: string[] };

/** One coordination pass over the epic's children: answer known questions, escalate unknown ones to
 *  needs-input, harvest DONE/BLOCKED reports. Does not spawn. */
export async function coordinateOnce(epic: Work, ledger: Ledger, resolve: (name: string) => Promise<Runner> = runnerNamed): Promise<PassResult> {
  const res: PassResult = { answered: [], escalated: [], reviewed: [], blocked: [], waiting: [] };
  const children = ledger.tasks(epic.id).filter((t) => t.state === 'running' || t.state === 'needs-input');
  for (const w of children) {
    const session = w.session ?? w.claim;
    if (session === null) { res.waiting.push(w.id); continue; }
    const cwd = w.cwd ?? ledger.worktrees(epic.id).find((wt) => wt.kind === 'shared' && wt.state === 'active')?.path ?? '.';
    const h = { runner: w.runner ?? epic.runner ?? 'claude', session, ref: w.ref, cwd };
    const r = await resolve(h.runner);
    const texts = await r.transcript(h);
    const last = texts.at(-1) ?? '';
    const ask = last.match(/ASK:\s*(.+)/);
    if (ask !== null) {
      const question = ask[1]!.trim();
      ledger.addEvent(w.id, 'question', question);
      const answer = knownAnswer(question, epic, children, ledger);
      if (answer !== undefined) {
        await r.send(h, answer);
        ledger.addEvent(w.id, 'answer', answer);
        if (w.state === 'needs-input') ledger.transition(w.id, 'running');
        res.answered.push(w.id);
        continue;
      }
      if (w.state !== 'needs-input') ledger.transition(w.id, 'needs-input');
      res.escalated.push(w.id);
      continue;
    }
    const status = last.match(/STATUS:\s*(DONE|BLOCKED)/);
    if (status !== null && (w.state === 'running' || w.state === 'needs-input')) {
      if (w.state === 'needs-input') ledger.transition(w.id, 'running');
      ledger.addEvent(w.id, 'report', `${status[1]}\n${last}`);
      ledger.transition(w.id, status[1] === 'DONE' ? 'review' : 'blocked');
      (status[1] === 'DONE' ? res.reviewed : res.blocked).push(w.id);
      continue;
    }
    res.waiting.push(w.id);
  }
  return res;
}