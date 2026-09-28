import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import type { Card } from '../../taste/src/card.ts';
import type { Feedback } from './ledger.ts';

export type PromotionCandidate = {
  card: Card;
  evidence: Feedback[];
  projects: number;
  attached: number;
};

/** Adopted project-scoped cards whose evidence recurs: ≥2 projects or ≥2 real-world (attached) feedback. */
export function promotionCandidates(cards: Card[], feedback: Feedback[]): PromotionCandidate[] {
  const out: PromotionCandidate[] = [];
  for (const card of cards) {
    if (card.status !== 'adopted') continue;
    if (!card.scope.some((s) => s.startsWith('project:'))) continue;
    const evidence = feedback.filter((f) => f.card === card.id);
    const projects = new Set(evidence.flatMap((f) => (f.project !== null ? [f.project] : []))).size;
    const attached = evidence.filter((f) => f.source === 'attached').length;
    if (projects >= 2 || attached >= 2) out.push({ card, evidence, projects, attached });
  }
  return out.sort((a, b) => b.evidence.length - a.evidence.length);
}

/** Write a global *candidate* card (never adopted in one step) from a project card and its evidence. */
export async function adoptCard(card: Card, evidence: string[], cardsDir: string): Promise<string> {
  const dir = join(cardsDir, card.category);
  await mkdir(dir, { recursive: true });
  const text = `---
id: ${card.id}
title: ${card.title}
category: ${card.category}
scope: [global]
kind: ${card.kind}
status: candidate
always: false
enforce: []
evidence: [${evidence.join(', ')}]
---
${card.body}
`;
  const path = join(dir, `${card.id}.md`);
  await writeFile(path, text);
  return path;
}