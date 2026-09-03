import { readdir, readFile, mkdir, writeFile, rm } from 'node:fs/promises';
import { join } from 'node:path';
import { Categories, parseCard, type Card, type Category } from './card.ts';

export const CONSTITUTION_LIMIT = 2000;
export const FRAGMENT_BEGIN = '<!-- taste:begin -->';
export const FRAGMENT_END = '<!-- taste:end -->';

const skillDescriptions: Record<Category, string> = {
  judgment: 'Use before any analysis or design decision: how to size a need, a gap, a cost, a fix.',
  alternatives: 'Use when weighing options or when a settled decision resurfaces.',
  organisation: 'Use when naming, placing or structuring code, modules, tests or data access.',
  'types-and-schemas': 'Use when writing TypeScript types, parsing input, or touching a schema.',
  'defensive-coding': 'Use when tempted to add a check, wrapper, flag, catch, suppression or branch.',
  comments: 'Use when about to write a comment.',
  'working-method': 'Use when planning a task, delegating, testing, or about to claim something is done.',
  communication: 'Use when writing a reply, report or brief.',
};

export type Presets = { biome: Set<string>; eslint: Set<string> };

export async function loadCards(cardsDir: string): Promise<Card[]> {
  const cards: Card[] = [];
  for (const dir of await readdir(cardsDir)) {
    for (const f of await readdir(join(cardsDir, dir))) {
      if (!f.endsWith('.md')) continue;
      const path = join(cardsDir, dir, f);
      cards.push(parseCard(await readFile(path, 'utf8'), path));
    }
  }
  return cards.sort((a, b) => a.id.localeCompare(b.id));
}

export async function loadPresets(presetsDir: string): Promise<Presets> {
  const biome = JSON.parse(await readFile(join(presetsDir, 'biome/biome.json'), 'utf8')) as { linter: { rules: Record<string, unknown> } };
  const biomeIds = new Set<string>();
  for (const [group, rules] of Object.entries(biome.linter.rules)) {
    if (typeof rules === 'object' && rules !== null) for (const name of Object.keys(rules)) biomeIds.add(`${group}/${name}`);
  }
  const eslint = JSON.parse(await readFile(join(presetsDir, 'eslint/rules.json'), 'utf8')) as Record<string, unknown>;
  return { biome: biomeIds, eslint: new Set(Object.keys(eslint)) };
}

export const distributable = (cards: Card[]): Card[] =>
  cards.filter((c) => c.status === 'adopted' && !c.scope.some((s) => s.startsWith('project:')));

export function missingEnforcements(cards: Card[], presets: Presets): string[] {
  const missing: string[] = [];
  for (const c of cards) for (const e of c.enforce) {
    const [tool, rule] = e.split(/:(.+)/, 2);
    const ok = tool === 'biome' ? presets.biome.has(rule ?? '') : tool === 'eslint' ? presets.eslint.has(rule ?? '') : false;
    if (!ok) missing.push(`${c.id}: ${e}`);
  }
  return missing;
}

export function renderConstitution(cards: Card[]): string {
  const lines = cards.filter((c) => c.always).map((c) => `- **${c.title}.** ${c.statement}`);
  const text = `# Taste\nFull cards: /taste-* skills.\n${lines.join('\n')}\n`;
  if (text.length > CONSTITUTION_LIMIT) throw new Error(`constitution is ${text.length} chars, limit ${CONSTITUTION_LIMIT}`);
  return text;
}

export function renderSkill(category: Category, cards: Card[]): string {
  const sections = cards.filter((c) => c.category === category).map((c) => `## ${c.title}\n${c.body}\n`);
  return `---\nname: taste-${category}\ndescription: ${skillDescriptions[category]}\n---\n# Taste: ${category}\n\n${sections.join('\n')}`;
}

export const renderFragment = (constitution: string): string => `${FRAGMENT_BEGIN}\n${constitution}${FRAGMENT_END}\n`;

export type BuildPaths = { cardsDir: string; presetsDir: string; pluginDir: string; distDir: string };

export async function build(p: BuildPaths): Promise<{ cards: number; skills: string[] }> {
  const cards = distributable(await loadCards(p.cardsDir));
  const missing = missingEnforcements(cards, await loadPresets(p.presetsDir));
  if (missing.length > 0) throw new Error(`enforce ids absent from presets:\n${missing.join('\n')}`);
  const constitution = renderConstitution(cards);
  const skillsDir = join(p.pluginDir, 'skills');
  await rm(skillsDir, { recursive: true, force: true });
  const skills: string[] = [];
  for (const category of Categories) {
    if (!cards.some((c) => c.category === category)) continue;
    const dir = join(skillsDir, `taste-${category}`);
    await mkdir(dir, { recursive: true });
    await writeFile(join(dir, 'SKILL.md'), renderSkill(category, cards));
    skills.push(`taste-${category}`);
  }
  await mkdir(p.distDir, { recursive: true });
  await writeFile(join(p.pluginDir, 'constitution.md'), constitution);
  await writeFile(join(p.distDir, 'constitution.md'), constitution);
  await writeFile(join(p.distDir, 'AGENTS.fragment.md'), renderFragment(constitution));
  return { cards: cards.length, skills };
}
