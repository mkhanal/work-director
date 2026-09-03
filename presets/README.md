# Presets
Mechanical taste. Adopt one, nothing else in the repo changes.
- Biome: `"extends": ["./node_modules/@mkhanal/taste-presets/biome/biome.json"]` or copy `biome/biome.json` and extend it.
- ESLint flat config: `import taste from '@mkhanal/taste-presets/eslint/index.js'` and spread `taste.rules`.
- tsconfig: `"extends": "./node_modules/@mkhanal/taste-presets/tsconfig/base.json"`.
Every id in a card's `enforce:` must exist here; `bun run build` checks it.
