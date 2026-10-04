> **lazyspec.** Humans edit freely. Agents change this only through
> `/lazyspec`, with its tests, in one edit.
>
> Each `##` heading is one requirement. Its test repeats that heading
> as its own name — to find it, search the tests for that text.

# Runners (Go)

The runners own each provider's commands, session discovery, transcript
parsing and error messages. The three foundation adapters are code; every other
provider is a TOML file of commands under `~/.work-director/runners/`.

## A Role Takes The Cheapest Model That Also Does The Job
`Pick` chooses the model one role should use, and a role is a job rather than a
price: `interpret` classifies text into a closed vocabulary, `taste` judges
whether a rule generalises. The ladder is forced, then a preferred model that
meets the floor, then the cheapest model that meets the floor, then a runner's
own default when nothing could be ranked, then a refusal. Cheapest alone is not
the rule — a free but weak model judging whether a taste rule generalises
promotes bad taste globally, which is worse than not automating it — so
candidates are walked cheapest-first and probed in that order and the first that
passes is by construction the cheapest that can do the job. Probing in cost
order rather than probing everything is what keeps it cheap: it stops at the
first pass rather than paying to rank forty models. A model whose cost nobody has
stated is unrankable rather than free, because unknown is not zero and guessing
it as zero is how a judgement starts costing money without anyone deciding that
it should. A probe that could not be asked at all — no runner, no quota, no
session — is recorded as not reached, which is not the same as the model failing,
and the two never collapse. Every candidate tried and its verdict are returned,
so the ladder is inspectable rather than a result with no explanation.

## A Project Narrows The Policy And Can Never Widen It
A role's policy is its runners, the models it prefers, and optionally one model
that overrides both. `Resolve` narrows a machine's global policy with a project's
own and refuses anything a project asks for the global policy does not allow: a
runner list on the project is checked against the global list rather than
replacing it, and a forced model is refused when its runner is not among the
runners allowed here. The asymmetry is the whole of the safety property — a
project forbidden from reaching a provider cannot re-allow it in its own file,
or closing a provider globally would be one project file away from undone. A
project may forbid and may reorder; it may never permit. `PromoteGlobal` defaults
true, because global taste is the director's own engineering taste and travels
by design while project rules live in the project; it is available for a team
that wants even their own cards to stay put. Every refusal names what is
allowed instead, because a refusal that only says no is a dead end.

## A Model Outside The Allowed Runners Is Refused By Name
`RolePolicy.Allowed` reports whether a model may be used under a policy, and it
is what makes a `/model` switch checkable rather than decorative: the switch
changes the preference inside the allowlist and cannot leave it. A model whose
runner is not listed is refused naming the runners that are, and a role with a
forced model refuses anything but that model. This holds for agents as much as
for people, because the loop reads the same set.

## A Runner That Reports Nothing Is Asked Rather Than Guessed At
No runner's CLI reports what its models cost — opencode lists ids and no prices,
claude lists nothing at all — so cost comes from a declaration in
`$WD_HOME/models.json`, which is a person stating a number rather than wd
inferring one from a model name, because a name-based guess is right until the
free tier carrying that name is replaced. An absent file is not an error. With
nothing declared and nothing rankable, `Pick` returns that runner with no model
named, so the runner's own default is used: the provider's choice is very likely
what a person would have got, and is never a guess wd invented. Every model name
the ladder carries is either detected from a runner's own listing or declared by
a person; none is hardcoded, because a hardcoded free-tier id is right for about
as long as that tier exists.

## Spawning Records Runner Session And Attach Hint
`spawn` returns a handle with the runner's session id and a hint a human can run to join the session.

## Sending Continues The Same Session
`send` addresses the runner with the handle's session id, never starting a new session.

## A Transcript Yields The Executor Messages
`transcript` returns the assistant texts of the session, oldest first, from the runner's own store.

## A Transcript Follows Its Session Into A Worktree
`transcript` finds a session by its id wherever the runner's store now keeps it, so a session that moved into a worktree after spawn still reports.

## A Transcript Skips A Line Still Being Written
Reading a runner's jsonl store, `transcript` skips a final line with no
newline yet, which the executor is still writing, and fails on a complete
line that does not parse.

## A Runner Forwards The Chosen Model
Providers whose CLI takes a `--model` flag get it on spawn, forwarded as-is; a runner whose command line cannot take one fails loud instead of dropping the choice.

## Model Lists Come From The Provider CLI, Not A Registry
`models()` shells the provider's own model-listing command (`opencode models`, `codex debug models`) and returns whatever lines it prints. No model ids are hardcoded in the director; a provider with no list command returns none, never a guess.

## A Runner Can Be Defined By A File Of Commands
Every runner besides the three foundation adapters is a TOML file under `~/.work-director/runners/` listing the commands to spawn, send, check status and list models; any provider whose CLI fits that shape works with no code change, and `wd runner init` writes the starter file.

## Every Registered Runner Reports Whether It Is Detected
Each built-in and spec-file runner names the command it runs (a spec's is
the first word of its spawn line); it is detected, with the resolved path,
when that command is on PATH. Not detected is a fact, never an error.

## Only A Detected Runner Resolves For Spawning
Resolving a runner to spawn fails when its command is not on PATH. The
error names the runner and its command, says it was not found on PATH, and
points at `wd runner init` for a provider that is not built in.

## A Runner File That Cannot Work Is Rejected When Added
A runner file whose regexes do not compile, or whose commands use a
placeholder that command cannot fill, fails `wd runner add` naming the regex
or placeholder, and is never written.
