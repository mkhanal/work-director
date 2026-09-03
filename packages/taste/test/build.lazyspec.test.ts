import { describe, expect, test } from 'bun:test';
import { mkdtemp, mkdir, writeFile, readFile, readdir } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { parseCard, CardError } from '../src/card.ts';
import { build, renderConstitution, CONSTITUTION_LIMIT, FRAGMENT_BEGIN, FRAGMENT_END } from '../src/build.ts';

const card = (o: Partial<Record<string, string>> = {}, body = 'Statement.\n**Why:** w\n**Apply:** a') =>
  `---\nid: ${o.id ?? 'x'}\ntitle: ${o.title ?? 'Title X'}\ncategory: ${o.category ?? 'judgment'}\nscope: ${o.scope ?? '[global]'}\nkind: ${o.kind ?? 'principle'}\nstatus: ${o.status ?? 'adopted'}\nalways: ${o.always ?? 'false'}\nenforce: ${o.enforce ?? '[]'}\nevidence: []\n---\n${body}\n`;

async function fixture(cards: Record<string, string>, biomeRules = { style: { noEnum: 'error' } }) {
  const root = await mkdtemp(join(tmpdir(), 'taste-'));
  for (const [rel, text] of Object.entries(cards)) {
    await mkdir(join(root, 'cards', rel.split('/')[0] ?? ''), { recursive: true });
    await writeFile(join(root, 'cards', rel), text);
  }
  await mkdir(join(root, 'presets/biome'), { recursive: true });
  await mkdir(join(root, 'presets/eslint'), { recursive: true });
  await writeFile(join(root, 'presets/biome/biome.json'), JSON.stringify({ linter: { rules: biomeRules } }));
  await writeFile(join(root, 'presets/eslint/rules.json'), JSON.stringify({ 'no-else-return': 'error' }));
  const paths = { cardsDir: join(root, 'cards'), presetsDir: join(root, 'presets'), pluginDir: join(root, 'plugin'), distDir: join(root, 'dist') };
  return { root, paths };
}

describe('A Card Missing A Required Field Is Rejected', () => {
  test('missing kind names the path and field', () => {
    const text = card().replace(/kind: .*\n/, '');
    expect(() => parseCard(text, 'c/x.md')).toThrow(new CardError('c/x.md', 'missing field kind'));
  });
});

describe('Only Adopted Non-Project Cards Reach The Artifacts', () => {
  test('candidate, retired and project cards are excluded', async () => {
    const { paths } = await fixture({
      'judgment/a.md': card({ id: 'a', title: 'Adopted One', always: 'true' }),
      'judgment/b.md': card({ id: 'b', title: 'Candidate One', status: 'candidate', always: 'true' }),
      'judgment/c.md': card({ id: 'c', title: 'Retired One', status: 'retired' }),
      'judgment/d.md': card({ id: 'd', title: 'Project One', scope: '[project:foo]', always: 'true' }),
    });
    await build(paths);
    const skill = await readFile(join(paths.pluginDir, 'skills/taste-judgment/SKILL.md'), 'utf8');
    const constitution = await readFile(join(paths.pluginDir, 'constitution.md'), 'utf8');
    expect(skill).toContain('Adopted One');
    for (const t of ['Candidate One', 'Retired One', 'Project One']) { expect(skill).not.toContain(t); expect(constitution).not.toContain(t); }
  });
});

describe('Every Category With Cards Becomes One Skill', () => {
  test('two categories yield two skills, each with its titles', async () => {
    const { paths } = await fixture({
      'judgment/a.md': card({ id: 'a', title: 'Judge A' }),
      'comments/b.md': card({ id: 'b', title: 'Comment B', category: 'comments' }),
    });
    const out = await build(paths);
    expect(out.skills.sort()).toEqual(['taste-comments', 'taste-judgment']);
    expect(await readdir(join(paths.pluginDir, 'skills'))).toHaveLength(2);
    expect(await readFile(join(paths.pluginDir, 'skills/taste-comments/SKILL.md'), 'utf8')).toContain('## Comment B');
  });
});

describe('The Constitution Holds Only Always Cards And Stays Under The Limit', () => {
  test('always cards appear as title plus statement; others do not', () => {
    const a = parseCard(card({ id: 'a', title: 'Always A', always: 'true' }, 'Do A.\n**Why:** w'), 'a');
    const b = parseCard(card({ id: 'b', title: 'Never B' }), 'b');
    const text = renderConstitution([a, b]);
    expect(text).toContain('- **Always A.** Do A.');
    expect(text).not.toContain('Never B');
  });
  test('over the limit fails', () => {
    const big = parseCard(card({ id: 'big', title: 'Big', always: 'true' }, 'x'.repeat(CONSTITUTION_LIMIT)), 'big');
    expect(() => renderConstitution([big])).toThrow(/limit 2000/);
  });
});

describe('An Enforce Id Absent From The Presets Fails The Build', () => {
  test('unknown biome rule aborts naming card and id', async () => {
    const { paths } = await fixture({ 'judgment/a.md': card({ id: 'a', enforce: '[biome:style/noEnum, biome:style/noSuchRule]' }) });
    await expect(build(paths)).rejects.toThrow(/a: biome:style\/noSuchRule/);
  });
  test('known biome and eslint rules pass', async () => {
    const { paths } = await fixture({ 'judgment/a.md': card({ id: 'a', enforce: '[biome:style/noEnum, eslint:no-else-return]' }) });
    await expect(build(paths)).resolves.toBeDefined();
  });
});

describe('The Agents Fragment Is Wrapped In Taste Markers', () => {
  test('fragment carries the constitution between markers', async () => {
    const { paths } = await fixture({ 'judgment/a.md': card({ id: 'a', title: 'Always A', always: 'true' }) });
    await build(paths);
    const fragment = await readFile(join(paths.distDir, 'AGENTS.fragment.md'), 'utf8');
    const constitution = await readFile(join(paths.distDir, 'constitution.md'), 'utf8');
    expect(fragment.startsWith(FRAGMENT_BEGIN)).toBe(true);
    expect(fragment.trimEnd().endsWith(FRAGMENT_END)).toBe(true);
    expect(fragment).toContain(constitution);
  });
});
