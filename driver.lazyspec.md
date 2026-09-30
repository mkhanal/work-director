# driver

An unattended goal loop with a stop condition. The stop condition is the point:
autonomy without one is a loop that runs until something is killed, which is not
autonomy, it is an absence of attention.

## A Run Stops On A Condition The Ledger Already Holds
`Drive` never asks whether to continue. It checks every bound before each turn
rather than after, so a budget of nothing stops without spending one more pass
first. `StopComplete` means every task has come to rest, and "came to rest" is
not "nothing is running": a goal whose tasks are all still queued has not been
driven, and calling that complete would report a run that never ran. `StopBudget`
means a bound ran out with work left. `StopStalled` means nothing moved for the
bound number of turns. `StopFailed` means the loop could not run, with why.
Every one of these is a fact about work, not a judgement about it, and every one
says which fact ran out: "it stopped" is not something a person can act on. Only
`StopComplete` reports `Shipped`. What the run finished is computed from the
open set rather than reported by the pass, because a pass moves work between
states and only the ledger sees a task come to rest.

## A Run Judges A Question In A Person's Place, Up To A Bound
Each turn runs a coordination pass, and the questions the pass could not answer
go to the judge — with the work that is settled, so the judgement is made from
what the project has already settled. The bound on judgements is the tightest
one by default, because it counts how often the loop is allowed to act in a
person's place; a run that needs a hundred human decisions was never
autonomous. A question beyond the bound is not dropped: it becomes the list of
where a person is still needed. A judge that could not run is a failure and is
reported as one, not quietly counted as an answer.

## A Refusal Is An Answer, And It Is Retried Never
A verdict with a decline is recorded, counted against the bound, and left
unanswered. Retrying a question a model has declined to settle spends the run's
budget on a question it has already said it cannot answer. A hedged reply counts
as a refusal for the same reason.

## A Judgement Is Recorded Before It Is Delivered
A judgement is recorded on the work before the answer is handed to the executor
waiting on it, and the spend is what the turn actually reported rather than
what the driver meant to do, so the budget cannot drift from reality. An
unrecorded decision is one the review surface cannot show a reader, and a
judgement that is delivered but not recorded is a decision nobody can audit.

## A Run Reports What A Person Is Still Needed For
The result carries the questions it settled, the questions it could not and the
tasks that were still open. Unanswered is empty only when the goal shipped, so
a run that ended honestly is one whose bill says exactly where a person is
still required — and a caller ending a goal that did not ship can read that
bill instead of inferring what stopped from silence.

## A Driver With No Judge Is The Old Supervised Behaviour
With no judge wired, every question stays unanswered and the run stops at the
first one, naming it. That is not a degraded mode to be apologised for: it is
the behaviour before judgement existed, it works, and it is honest about where
it stopped. What a driver must never do is decide without spending from a
bound that says how much deciding was allowed.
