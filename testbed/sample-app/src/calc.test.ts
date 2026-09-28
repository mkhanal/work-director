import { describe, expect, test } from 'bun:test';
import { add } from './calc.ts';

describe('add', () => {
  test('1 + 2 = 3', () => expect(add(1, 2)).toBe(3));
  test('is associative', () => expect(add(add(1, 2), 3)).toBe(add(1, add(2, 3))));
});