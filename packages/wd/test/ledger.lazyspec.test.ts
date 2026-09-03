import { describe, expect, test } from 'bun:test';
import { IllegalTransition, Ledger, NotReady } from '../src/ledger.ts';

const fresh = () => new Ledger(':memory:');

describe('New Work Starts Queued', () => {
  test('add returns queued work with a state event', () => {
    const l = fresh(), w = l.add('p', 'Do thing');
    expect(w.state).toBe('queued');
    expect(l.events(w.id, 'state').map((e) => e.body)).toEqual(['queued']);
  });
});

describe('Only Listed Transitions Are Allowed', () => {
  test('happy path walks every state', () => {
    const l = fresh(), { id } = l.add('p', 't');
    for (const s of ['briefed', 'running', 'review'] as const) l.transition(id, s);
    l.addEvent(id, 'report', 'DONE'); l.addEvent(id, 'verify', 'pass');
    expect(l.softDone(id, false).state).toBe('soft-done');
    expect(l.transition(id, 'done').state).toBe('done');
  });
  test('queued → done throws naming both states', () => {
    const l = fresh(), { id } = l.add('p', 't');
    expect(() => l.transition(id, 'done')).toThrow(new IllegalTransition('queued', 'done'));
  });
  test('done is terminal', () => {
    const l = fresh(), { id } = l.add('p', 't');
    l.transition(id, 'dropped');
    expect(() => l.transition(id, 'queued')).toThrow(IllegalTransition);
  });
});

describe('Soft Done Requires A Done Report And A Passing Verify', () => {
  test('lists what is missing', () => {
    const l = fresh(), { id } = l.add('p', 't');
    for (const s of ['briefed', 'running', 'review'] as const) l.transition(id, s);
    expect(() => l.softDone(id, false)).toThrow(new NotReady(['DONE report', 'passing verify']));
    l.addEvent(id, 'report', 'BLOCKED\nx'); l.addEvent(id, 'verify', 'fail\nx');
    expect(() => l.softDone(id, false)).toThrow(NotReady);
    l.addEvent(id, 'report', 'DONE\nok'); l.addEvent(id, 'verify', 'pass\nok');
    expect(l.softDone(id, false).state).toBe('soft-done');
  });
});

describe('Code Changes Need A Pull Request Before Soft Done', () => {
  test('pr event required when code changed', () => {
    const l = fresh(), { id } = l.add('p', 't');
    for (const s of ['briefed', 'running', 'review'] as const) l.transition(id, s);
    l.addEvent(id, 'report', 'DONE'); l.addEvent(id, 'verify', 'pass');
    expect(() => l.softDone(id, true)).toThrow(new NotReady(['pull request']));
    l.addEvent(id, 'pr', 'https://github.com/x/y/pull/1');
    expect(l.softDone(id, true).state).toBe('soft-done');
  });
});

describe('Feedback Seen Twice Becomes A Distill Candidate', () => {
  test('groups by card, then project, then global; singles excluded', () => {
    const l = fresh();
    l.addFeedback('a1', { card: 'parse-at-boundary' }); l.addFeedback('a2', { card: 'parse-at-boundary' });
    l.addFeedback('b1', { project: 'send-frugal' }); l.addFeedback('b2', { project: 'send-frugal' });
    l.addFeedback('only once', { card: 'fail-loud' }); l.addFeedback('g1'); l.addFeedback('g2');
    expect(l.distill()).toEqual([
      { key: 'card:parse-at-boundary', count: 2, texts: ['a1', 'a2'] },
      { key: 'project:send-frugal', count: 2, texts: ['b1', 'b2'] },
      { key: 'global', count: 2, texts: ['g1', 'g2'] },
    ]);
  });
});
