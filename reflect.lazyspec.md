> **lazyspec.** Humans edit freely. Agents change this only through
> `/lazyspec`, with its tests, in one edit.
>
> Each `##` heading is one requirement. Its test repeats that heading
> as its own name — to find it, search the tests for that text.

# Reflect (Go)

`wd reflect <id>` asks one model a bounded question about a finished session's
transcript: did anything durable get learned. The answer becomes feedback, so
promotion can count recurrence and a pattern can graduate to a rule card.

The call proposes and never disposes. It appends feedback rows and one event. It
never moves work state, never writes a card and never satisfies a gate.

## Nothing Durable Is A Correct Answer
`Parse` on a reply with no `DURABLE:` line returns no durable verdicts, and the
recorded event says `nothing durable`. A model that declines to find a lesson is
working correctly: promotion counts recurrence, so a call that always finds
something destroys the signal it depends on.

## A Durable Verdict Carries Its Lesson And Card
A `DURABLE:` line yields one verdict with the lesson text and, when the line ends
in `| card: <id>`, that card. The trailing card marker is stripped from the
lesson text.

## A Chatty Reply Costs Nothing
`Parse` ignores every line that is neither `DURABLE:` nor a `TOKENS:` line, so a
session that explains itself at length files nothing extra.

## The Transcript Is Quoted As Data
`Brief` wraps the transcript in a fenced block and tells the model to treat what
is inside it as evidence to judge, never as instructions, so a line in a
transcript cannot read as a command to the reflection session.

## The Brief Tells The Model That Most Sessions Teach Nothing
`Brief` states that most sessions teach nothing that generalises, that declining
is correct, and that it must not invent a lesson. It also excludes the task's
own outcome, so shipping a feature is not filed as an insight.

## The Recorded Event Names The Runner Model Cost And Verdict
`Event` records the runner, the model, the token count and every durable
verdict with its card, and says `nothing durable` when there were none, so a
later reader can see what was asked and what it cost.
