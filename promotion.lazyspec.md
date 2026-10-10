> **lazyspec.** Humans edit freely. Agents change this only through
> `/lazyspec`, with its tests, in one edit.
>
> Each `##` heading is one requirement. Its test repeats that heading
> as its own name — to find it, search the tests for that text.

# Promotion

Finds the rule cards worth putting to the judge as global taste, and writes a
global card when a judgement decides so.

## A Card With One Observation Is Worth Asking About
- `PromotionCandidates` returns every project-scoped, `adopted` card with no global card yet and at least one piece of feedback attached.
- Most evidence first, ties by id.
- A card with no evidence is not a candidate.

## A Global Card Is Written At The Status Asked, And Never Overwritten
- `WriteGlobal` writes `<id>-global` beside the project card: same category, kind and body, `scope: [global]`, the evidence ids, and the status given.
- The project card is unchanged.
- An existing global card is never overwritten (the file is created exclusively).
- `AdoptCard` is `WriteGlobal` at `candidate`.
