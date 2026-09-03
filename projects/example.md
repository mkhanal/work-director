---
path: ~/work/example-app
runner: claude
mode: ask
stack: [ts, biome, vitest]
workflows: [/lazyspec, /lazyspec-validate]
verify: [pnpm check, pnpm test]
instructions_file: AGENTS.md
default_branch: main
---
Roadmap, most important first. The director reads this file; it never edits the repo it describes.
