# Taste build

## A Card Missing A Required Field Is Rejected
Frontmatter without id, title, category, scope, kind, status or always fails with the card path and the field.

## Only Adopted Non-Project Cards Reach The Artifacts
Candidate and retired cards, and cards with any `project:` scope, are absent from skills and constitution.

## Every Category With Cards Becomes One Skill
`plugin/skills/taste-<category>/SKILL.md` exists for each category holding at least one distributable card, and contains every such card's title.

## The Constitution Holds Only Always Cards And Stays Under The Limit
Constitution lists exactly the `always: true` cards as title plus statement; rendering above 2000 characters fails.

## An Enforce Id Absent From The Presets Fails The Build
Any `enforce:` entry whose rule is not in `presets/biome/biome.json` or `presets/eslint/rules.json` aborts the build naming card and id.

## The Agents Fragment Is Wrapped In Taste Markers
`dist/AGENTS.fragment.md` starts with `<!-- taste:begin -->`, ends with `<!-- taste:end -->`, and carries the constitution between them.
