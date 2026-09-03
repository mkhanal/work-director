export type Frontmatter = { fields: Map<string, string>; body: string };

export function parseFrontmatter(text: string): Frontmatter | undefined {
  const m = text.match(/^---\n([\s\S]*?)\n---\n?([\s\S]*)$/);
  if (!m || m[1] === undefined || m[2] === undefined) return undefined;
  const fields = new Map<string, string>();
  for (const line of m[1].split('\n')) {
    const i = line.indexOf(':');
    if (i < 1) return undefined;
    fields.set(line.slice(0, i).trim(), line.slice(i + 1).trim());
  }
  return { fields, body: m[2].trim() };
}

export const list = (raw: string): string[] => {
  const inner = raw.replace(/^\[|\]$/g, '').trim();
  return inner === '' ? [] : inner.split(',').map((s) => s.trim());
};
