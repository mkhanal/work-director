import { resolve } from 'node:path';
import { build } from './build.ts';

const root = resolve(import.meta.dir, '../../..');
const cmd = process.argv[2];
if (cmd !== 'build') { console.error('usage: taste build'); process.exit(2); }
const out = await build({ cardsDir: resolve(root, 'taste/cards'), presetsDir: resolve(root, 'presets'), pluginDir: resolve(root, 'plugin'), distDir: resolve(root, 'dist') });
console.log(`${out.cards} cards → ${out.skills.length} skills, plugin/constitution.md, dist/AGENTS.fragment.md`);
