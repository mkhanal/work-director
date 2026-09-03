import { describe, expect, test } from 'bun:test';
import { parseCard } from '../../taste/src/card.ts';
import { HumanHoursRejected, REPORT_FORMAT, cardsFor, compose } from '../src/brief.ts';
import type { Work } from '../src/ledger.ts';
import { parseProject } from '../src/project.ts';

const project = parseProject(`---\npath: /tmp/x\nrunner: claude\nmode: ask\nstack: [ts, biome]\nworkflows: [/lazyspec, /review]\nverify: [bun test, bunx tsc --noEmit]\n---\nRoadmap.`, 'projects/x.md');
const card = (id: string, scope: string, status = 'adopted') => parseCard(`---\nid: ${id}\ntitle: T ${id}\ncategory: judgment\nscope: [${scope}]\nkind: principle\nstatus: ${status}\nalways: false\nenforce: []\nevidence: []\n---\nS ${id}.`, id);
const work = (title: string, detail = ''): Work => ({ id: 'ab12cd', project: 'x', title, detail, kind: 'task', state: 'queued', runner: null, session: null, ref: null, cwd: null, created: '', updated: '' });

describe('A Brief Carries Only Cards Whose Scope Matches The Project', () => {
  test('project:x, lang:ts, stack:biome in; global, lang:py, project:y, candidate out', () => {
    const cards = [card('g', 'global'), card('px', 'project:x'), card('ts', 'lang:ts'), card('bi', 'stack:biome'), card('py', 'lang:py'), card('py2', 'project:y'), card('c', 'lang:ts', 'candidate')];
    expect(cardsFor(project, cards).map((c) => c.id)).toEqual(['px', 'ts', 'bi']);
    const text = compose(work('Goal'), project, cards);
    expect(text).toContain('T ts'); expect(text).not.toContain('T py'); expect(text).not.toContain('T g');
    expect(text).toContain('taste constitution and /taste-* skills');
  });
});

describe('A Brief Names The Project Workflows And Verify Commands', () => {
  test('workflows, verify and instructions file present', () => {
    const text = compose(work('Goal'), project, []);
    expect(text).toContain('/lazyspec, /review');
    expect(text).toContain('`bun test`, `bunx tsc --noEmit`');
    expect(text).toContain('AGENTS.md');
  });
});

describe('Human Hour Estimates Are Rejected From Briefs', () => {
  test('quotes the match', () => {
    expect(() => compose(work('Fix later', 'proper fix is 8 hours'), project, [])).toThrow(new HumanHoursRejected('8 hours'));
    expect(() => compose(work('Estimate 3 story points'), project, [])).toThrow(HumanHoursRejected);
    expect(() => compose(work('Retention is 30 days'), project, [])).not.toThrow();
  });
});

describe('A Brief Ends With The Report Format', () => {
  test('report format is the tail', () => {
    expect(compose(work('Goal'), project, []).trimEnd().endsWith(REPORT_FORMAT)).toBe(true);
  });
});

describe('A Brief Carries Context And A Default For Ambiguity', () => {
  test('roadmap, decisions, history and the two checkpoints are present', () => {
    const text = compose(work('Goal'), project, [], { decisions: ['Use Kysely, not Drizzle'], history: ['ab12cd blocked on flaky CI 2026-09-01'] });
    expect(text).toContain('Roadmap.');
    expect(text).toContain('- Use Kysely, not Drizzle');
    expect(text).toContain('- ab12cd blocked on flaky CI 2026-09-01');
    expect(text).toContain('Decide ambiguities yourself');
    expect(text).toContain('Checkpoint: after your plan (before code) and at DONE');
  });
});
