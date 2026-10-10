> **lazyspec.** Humans edit freely. Agents change this only through
> `/lazyspec`, with its tests, in one edit.
>
> Each `##` heading is one requirement. Its test repeats that heading
> as its own name — to find it, search the tests for that text.

# Find

One bounded model call that names the existing goals a new request continues,
so a person resumes the goal they meant instead of starting a duplicate.

## The Model Is Shown The Goals And Told No Match Is Expected
- `Brief` lists every goal it is given with its id, state and title, and quotes the request in a fence as data, never as instructions.
- It asks for `MATCH: <id> | <why>` lines, most relevant first, at most three, or `NONE`.
- It says a new idea matches nothing and that no match is a correct answer.

## Only A Goal That Was Shown Can Match
- `Parse` reads `MATCH:` lines and ignores every other line.
- A match naming a goal that was not shown, or one already matched, is dropped.
- At most three matches are kept, in the order given.
