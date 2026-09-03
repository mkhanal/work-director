# Where our requirements live

- **packages/taste** — `packages/taste/specs/*.lazyspec.md`
  What the build produces from rule cards: which cards reach which artifact, and what
  a card must contain. Not the wording of any rule.

- **packages/wd** — `packages/wd/specs/*.lazyspec.md`
  What the director CLI guarantees at its boundary: ledger states and transitions,
  brief contents, what each runner adapter records. Not how a runner works internally.
