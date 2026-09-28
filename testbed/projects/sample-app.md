---
path: TESTBED/run/sample-app
runner: claude
mode: ask
stack: [ts]
workflows: [/lazyspec]
verify: [bun test]
instructions_file: AGENTS.md
default_branch: main
---
A sample app for validating the director: add an epic, split tasks, claim and merge. Each
command you try here is a dry run; the repo is disposable and rebuilt by `bun testbed/setup.ts`.