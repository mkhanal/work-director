import { describe, expect, test } from 'bun:test';
import { mkdtemp, readFile, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { parseCard } from '../../taste/src/card.ts';
import type { Feedback } from '../src/ledger.ts';
import { adoptCard, promotionCandidates } from '../src/promotion.ts';

const card = (id: string, scope = 'project:send-frugal') => parseCard(`---\nid: ${id}\ntitle: ${id}\ncategory: judgment\nscope: [${scope}]\nkind: principle\nstatus: adopted\nalways: false\nenforce: []\nevidence: []\n---\nStatement ${id}.\n**Why:** because.\n**Apply:** do.`, id);
const fb = (n: number, o: Partial<Feedback>): Feedback => ({ id: n, text: `${n}`, project: null, card: null, source: 'director', at: '', ...o });

describe('Only Adopted Project Cards With Recurring Evidence Graduate', () => {
  test('two projects on one project card graduate; singles do not', () => {
    const cards = [card('recurs'), card('singleton'), card('adopted-global', 'global')];
    const feedback = [
      fb(1, { card: 'recurs', project: 'a', source: 'attached' }),
      fb(2, { card: 'recurs', project: 'b', source: 'attached' }),
      fb(3, { card: 'singleton', project: 'a' }),
    ];
    const candidates = promotionCandidates(cards, feedback);
    expect(candidates.map((c) => c.card.id)).toEqual(['recurs']);
    expect(candidates[0]?.projects).toBe(2);
  });
  test('two attached feedback on a single project also graduate', () => {
    const feedback = [fb(1, { card: 'recurr', project: 'a', source: 'attached' }), fb(2, { card: 'recurr', project: 'a', source: 'attached' })];
    expect(promotionCandidates([card('recurr')], feedback).map((c) => c.card.id)).toEqual(['recurr']);
  });
  test('candidate-status cards never graduate', () => {
    const notAdopted = parseCard('---\nid: x\ntitle: X\ncategory: judgment\nscope: [project:foo]\nkind: principle\nstatus: candidate\nalways: false\nenforce: []\nevidence: []\n---\nX.', 'x');
    expect(promotionCandidates([notAdopted], [fb(1, { card: 'x', project: 'a' }), fb(2, { card: 'x', project: 'b' })])).toEqual([]);
  });
});

describe('Adopting A Candidate Writes A Global Candidate Card', () => {
  test('file written as global candidate with evidence, never adopted', async () => {
    const dir = join(await mkdtemp(join(tmpdir(), 'promo-')), 'cards');
    const c = card('send-frugal');
    const path = await adoptCard(c, ['1', '2'], dir);
    const text = await readFile(path, 'utf8');
    expect(text).toContain('scope: [global]');
    expect(text).toContain('status: candidate');
    expect(text).toContain('enforce: []');
    expect(text).toContain('evidence: [1, 2]');
    expect(text).toContain('Statement send-frugal.');
    expect(path).toContain(join('judgment', 'send-frugal.md'));
  });
});