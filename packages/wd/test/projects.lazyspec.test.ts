import { describe, expect, test } from 'bun:test';
import { installLazyspec, projectTemplate, parseProject } from '../src/project.ts';

describe('A New Project File Carries No Lazyspec Claim', () => {
  test('template has concrete frontmatter, no lazyspec field, and parses back', () => {
    const text = projectTemplate('shop', '/work/shop', { runner: 'opencode', mode: 'auto', stack: ['ts'], workflows: ['/lazyspec'], verify: ['bun test'] });
    expect(text).toContain('path: /work/shop');
    expect(text).toContain('runner: opencode');
    expect(text).toContain('mode: auto');
    expect(text).not.toMatch(/^lazyspec:.*$/m);
    expect(text).toContain('workflows: [/lazyspec]');
    expect(() => parseProject(text, 'projects/shop.md')).not.toThrow();
    expect(parseProject(text, 'projects/shop.md').name).toBe('shop');
  });
});

describe('Confirming The Preferred Lazyspec Files An Install Work Item', () => {
  test('returns an evolution target naming install in the repo, and no director-side claim', () => {
    const item = installLazyspec('Adopt the director\'s preferred lazyspec');
    expect(item.title).toBe('Adopt the director\'s preferred lazyspec');
    expect(item.detail).toContain('`*.lazyspec.md`');
    expect(item.detail).toContain('agent files');
    expect(item.detail).not.toContain('lazyspec: true');
  });
});

describe('A New Project Defaults To Auto Mode And Records A Chosen Model', () => {
  test('no options → mode auto; a chosen model is written and parsed back; blank model is none', () => {
    const t = projectTemplate('shop', '/work/shop');
    expect(t).toContain('mode: auto');
    expect(parseProject(t, 'projects/shop.md').mode).toBe('auto');
    expect(parseProject(t, 'projects/shop.md').model).toBeUndefined();
    const tm = projectTemplate('shop', '/work/shop', { model: 'fable' });
    expect(tm).toContain('model: fable');
    expect(parseProject(tm, 'projects/shop.md').model).toBe('fable');
    expect(parseProject('---\npath: /a\nrunner: claude\nstack: []\nworkflows: []\nverify: []\n---\n', 'projects/a.md').mode).toBe('auto');
  });
});