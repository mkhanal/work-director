import type { Card } from '../../taste/src/card.ts';
import type { Project } from './project.ts';
import type { Work } from './ledger.ts';

export const REPORT_FORMAT = `## Report format (last message, exactly)
STATUS: DONE | BLOCKED | NEEDS-INPUT
FILES: <changed files or none>
VERIFY: <command> → <exit code>, <lines that matter>
PR: <url or none>
NOTES: <one line; agent-minutes spent>`;

const humanHours = /\b\d+(\.\d+)?\s*(h|hrs?|hours?)\b|\bstory points?\b|\b(man|person)[- ](day|hour)s?\b/i;

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

export function compose(work: Work, project: Project, cards: Card[], ctx: Context = {}): string {
  assertNoHumanHours(`${work.title}\n${work.detail}`);
  const decisions = (ctx.decisions ?? []).map((d) => `- ${d}`).join('\n') || '- none yet';
  const history = (ctx.history ?? []).map((h) => `- ${h}`).join('\n') || '- none';
  const roadmap = (ctx.roadmap ?? project.roadmap).trim() || 'not written';
  const chosen = cardsFor(project, cards);
  const rules = chosen.map((c) => `- **${c.title}.** ${c.statement}`).join('\n');
  const workflows = project.workflows.length > 0 ? project.workflows.join(', ') : 'none listed';
  const verify = project.verify.length > 0 ? project.verify.map((v) => `\`${v}\``).join(', ') : 'none listed';
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
- Repo: ${project.path} (branch ${project.defaultBranch}). Follow the repo's own instructions in ${project.instructionsFile}.
- Workflows to use: ${workflows}.
- Verify before reporting: ${verify}.
- Mode ${project.mode}: ${project.mode === 'ask' ? 'do not open a PR; stop at review with the diff ready.' : 'open a draft PR when verify passes.'}
- Cost is agent minutes; never justify a shortcut with a human-hour estimate.
- Facts you can look up, you look up. NEEDS-INPUT is for value judgments only, and carries your proposed answer.
- Every message to the director is ≤10 lines. No narration, no restating the brief.

## Rules
Global rules are in your taste constitution and /taste-* skills. Specific to this project and stack:
${rules || '- none beyond the constitution'}

${REPORT_FORMAT}
`;
}
