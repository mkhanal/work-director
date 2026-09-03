export const Categories = ['judgment','alternatives','organisation','types-and-schemas','defensive-coding','comments','working-method','communication'] as const;
export type Category = (typeof Categories)[number];
export const Kinds = ['principle','practice','mechanical'] as const;
export type Kind = (typeof Kinds)[number];
export const Statuses = ['candidate','adopted','retired'] as const;
export type Status = (typeof Statuses)[number];
export type Scope = 'global' | `lang:${string}` | `stack:${string}` | `project:${string}` | `team:${string}`;

import { parseFrontmatter, list } from './frontmatter.ts';

export type Card = {
  id: string; title: string; category: Category; scope: Scope[]; kind: Kind; status: Status;
  always: boolean; enforce: string[]; evidence: string[]; statement: string; body: string; path: string;
};

export class CardError extends Error {
  constructor(readonly path: string, detail: string) { super(`${path}: ${detail}`); }
}

const isOneOf = <T extends readonly string[]>(values: T, v: string): v is T[number] => values.includes(v);
const scopeRe = /^(global|(lang|stack|project|team):[a-z0-9][a-z0-9-]*)$/;

export function parseCard(text: string, path: string): Card {
  const fm = parseFrontmatter(text);
  if (fm === undefined) throw new CardError(path, 'missing frontmatter');
  const { fields, body } = fm;
  const need = (k: string): string => {
    const v = fields.get(k);
    if (v === undefined || v === '') throw new CardError(path, `missing field ${k}`);
    return v;
  };
  const category = need('category'), kind = need('kind'), status = need('status'), always = need('always');
  if (!isOneOf(Categories, category)) throw new CardError(path, `unknown category ${category}`);
  if (!isOneOf(Kinds, kind)) throw new CardError(path, `unknown kind ${kind}`);
  if (!isOneOf(Statuses, status)) throw new CardError(path, `unknown status ${status}`);
  if (always !== 'true' && always !== 'false') throw new CardError(path, 'always must be true or false');
  const scope = list(need('scope'));
  const badScope = scope.find((s) => !scopeRe.test(s));
  if (scope.length === 0 || badScope !== undefined) throw new CardError(path, `bad scope ${badScope ?? '[]'}`);
  const statement = body.split('\n').find((l) => l.trim() !== '');
  if (statement === undefined) throw new CardError(path, 'empty body');
  return {
    id: need('id'), title: need('title'), category, kind, status, always: always === 'true',
    scope: scope.filter((s): s is Scope => scopeRe.test(s)),
    enforce: list(fields.get('enforce') ?? '[]'), evidence: list(fields.get('evidence') ?? '[]'),
    statement, body, path,
  };
}
