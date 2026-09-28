import type { Card } from '../../taste/src/card.ts';
import type { Project } from './project.ts';
import { splitImpact, type Work } from './ledger.ts';

export const REPORT_FORMAT = `## Report format (last message, exactly)
STATUS: DONE | BLOCKED | NEEDS-INPUT
FILES: <changed files or none>
VERIFY: <command> → <exit code>, <lines that matter>
PR: <url or none>
NOTES: <one line; agent-minutes spent>`;

const humanHours = /\b\d+(\.\d+)?\s*(h|hrs?|hours?)\b|\bstory points?\b|\b(man|person)[- ](day|hour)s?\b/i;

const COORDINATION = `You are an executor, not the whole team. Claim what you work, and let others see it:
- Claim your task: \`wd claim <id> <session>\`. Record every area you will touch: \`wd impact <id> <+path>\`.
- Look before stepping: \`wd conflict <epic>\` shows overlapping claims; never edit a path another active task claims.
- Something does not work or conflicts? Raise \`wd concern add <id> "<text>\` — a concern is a decision queue for the director, not something you work around silently.
- Own worktree for isolation? Register it: \`wd worktree attach <id> <path> [--branch <b>]\`. Merge it back when done: \`wd merge <id>\` (after verify and a clean conflict scan).`;

export class HumanHoursRejected extends Error {
  constructor(readonly match: string) { super(`human-hour estimate rejected: "${match}"`); }
}

export function assertNoHumanHours(text: string): void {
  const m = text.match(humanHours);
  if (m) throw new HumanHoursRejected(m[0]);
}

/** Global cards reach executors through the taste plugin; the brief carries only what the plugin cannot know: project and stack scope. */
export function cardsFor(project: Project, cards: Card[]): Card[] {
  const scopes = new Set([`project:${project.name}`, ...project.stack.flatMap((s) => [`lang:${s}`, `stack:${s}`])]);
  return cards.filter((c) => c.status === 'adopted' && c.scope.some((s) => scopes.has(s)));
}

export type Context = { roadmap?: string | undefined; decisions?: string[] | undefined; history?: string[] | undefined };

const shared = (project: Project): string => {
  const workflows = project.workflows.length > 0 ? project.workflows.join(', ') : 'none listed';
  const verify = project.verify.length > 0 ? project.verify.map((v) => `\`${v}\``).join(', ') : 'none listed';
  return `- Repo: ${project.path}; follow its own instructions in ${project.instructionsFile}.
- Workflows to use: ${workflows}.
- Verify before reporting: ${verify}.
- Mode ${project.mode}: ${project.mode === 'ask' ? 'do not open a PR; stop at review with the diff ready.' : 'open a draft PR when verify passes.'}
- Cost is agent minutes; never justify a shortcut with a human-hour estimate.
- Facts you can look up, you look up. NEEDS-INPUT is for value judgments only, and carries your proposed answer.
- Every message to the director is ≤10 lines. No narration, no restating the brief.
`;
};

const rules = (project: Project, cards: Card[]): string => {
  const chosen = cardsFor(project, cards);
  return chosen.length > 0 ? chosen.map((c) => `- **${c.title}.** ${c.statement}`).join('\n') : '- none beyond the constitution';
};

/** Standalone work: full context, single executor, own branch/PR. */
export function compose(work: Work, project: Project, cards: Card[], ctx: Context = {}): string {
  assertNoHumanHours(`${work.title}\n${work.detail}`);
  const decisions = (ctx.decisions ?? []).map((d) => `- ${d}`).join('\n') || '- none yet';
  const history = (ctx.history ?? []).map((h) => `- ${h}`).join('\n') || '- none';
  const roadmap = (ctx.roadmap ?? project.roadmap).trim() || 'not written';
  return `# Brief ${work.id} · ${project.name} · ${work.kind}

## Goal
${work.title}
${work.detail}

## Context
Where this project is heading:
${roadmap}
Decisions already made (do not re-open):
${decisions}
Relevant history:
${history}

## How to work
- Decide ambiguities yourself, consistent with the rules below; record each decision under NOTES. Stop only for irreversible actions.
- Checkpoint: after your plan (before code) and at DONE, send a report in the format below. Nothing else in between.
- Capabilities available: the repo's workflows and verify commands listed here, web research, the repo's own docs.

## Constraints
${shared(project)}

## Rules
Global rules are in your taste constitution and /taste-* skills. Specific to this project and stack:
${rules(project, cards)}

${REPORT_FORMAT}
`;
}

/** One task of an epic: epic goal, the task, and the other executors' live claims. No backlog, no history. */
export function composeSlice(work: Work, epic: Work, claims: Work[], project: Project, cards: Card[]): string {
  assertNoHumanHours(`${work.title}\n${work.detail}`);
  const claimLines = claims.length > 0
    ? claims.map((c) => `- ${c.id} ${c.title} (${c.claim}) — ${splitImpact(c.impact).join(', ') || 'no impact recorded'}`).join('\n')
    : '- none currently';
  return `# Brief ${work.id} · ${project.name} · ${work.heading ?? 'task'} of epic ${epic.id}

## Goal (epic)
${epic.title}
— this task:
${work.title}${work.detail ? `\n${work.detail}` : ''}

## Co-workers on this branch — do not touch these paths
${claimLines}
The full register is \`wd tasks ${epic.id}\`; before a shared path run \`wd conflict ${epic.id}\`. A resolved concern is a decision; follow it.

## How to work
${COORDINATION.replaceAll('<id>', work.id).replaceAll('<epic>', epic.id)}
- Decide ambiguities yourself, consistent with the rules below. Stop only for irreversible actions.
- Checkpoint: after your plan (before code) and at DONE, send a report in the format below. Nothing else in between.

## Constraints
${shared(project)}

## Rules
Global rules are in your taste constitution and /taste-* skills. Specific to this project and stack:
${rules(project, cards)}

${REPORT_FORMAT}
`;
}

/** An epic spawn brief: goal, context, and the compact open task list grouped by heading. */
export function composeEpic(epic: Work, taskBlock: string, project: Project, cards: Card[], ctx: Context = {}): string {
  assertNoHumanHours(`${epic.title}\n${epic.detail}`);
  const decisions = (ctx.decisions ?? []).map((d) => `- ${d}`).join('\n') || '- none yet';
  const history = (ctx.history ?? []).map((h) => `- ${h}`).join('\n') || '- none';
  const roadmap = (ctx.roadmap ?? project.roadmap).trim() || 'not written';
  return `# Brief ${epic.id} · ${project.name} · epic

## Goal
${epic.title}
${epic.detail}

## Context
Where this project is heading:
${roadmap}
Decisions already made (do not re-open):
${decisions}
Relevant history:
${history}

## Task list (open; claim with \`wd claim <id> <session>\`)
${taskBlock}

## How to work
${COORDINATION.replaceAll('<epic>', epic.id)}
- Decide ambiguities yourself, consistent with the rules below. Stop only for irreversible actions.
- Checkpoint: after your plan (before code) and at DONE, send a report in the format below. Nothing else in between.

## Constraints
${shared(project)}

## Rules
Global rules are in your taste constitution and /taste-* skills. Specific to this project and stack:
${rules(project, cards)}

${REPORT_FORMAT}
`;
}

/** `wd tasks` rendering grouped by heading, e.g. "- <id> <state> <title> (claim: <who>)". */
export function renderTaskBlock(tasks: Work[]): string {
  const byHeading = new Map<string, Work[]>();
  const unheaded: Work[] = [];
  for (const t of tasks) {
    if (t.heading !== null) byHeading.set(t.heading, [...(byHeading.get(t.heading) ?? []), t]);
    else unheaded.push(t);
  }
  const lines: string[] = [];
  for (const [heading, group] of [...byHeading].sort(([a], [b]) => a.localeCompare(b))) {
    lines.push(`## ${heading}`);
    for (const t of group) lines.push(renderTaskLine(t));
  }
  if (unheaded.length > 0) {
    lines.push('## (no heading)');
    for (const t of unheaded) lines.push(renderTaskLine(t));
  }
  return lines.join('\n') || '- none';
}

function renderTaskLine(t: Work): string {
  const claim = t.claim !== null ? ` (claim: ${t.claim})` : '';
  const impact = splitImpact(t.impact);
  return `- ${t.id} ${t.state} ${t.title}${claim}${impact.length > 0 ? ` — ${impact.join(', ')}` : ''}`;
}