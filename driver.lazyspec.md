> **lazyspec.** Humans edit freely. Agents change this only through
> `/lazyspec`, with its tests, in one edit.
>
> Each `##` heading is one requirement. Its test repeats that heading
> as its own name — to find it, search the tests for that text.

# Driver

`Drive` runs a goal's loop unattended until a stop condition the ledger holds.
Each turn runs one coordination pass, then judges what the pass could not answer.

## A Run Stops On A Condition The Ledger Already Holds
- Every bound is checked before a turn, so a bound of zero spends no turn.
- `Drive` never asks whether to continue.
- `StopComplete`: every task has come to rest.
- `StopBudget`: a bound (turns, judgements, tokens, deadline) ran out with work left; `Why` names the bound.
- `StopFailed`: the loop could not run; `Why` carries the failure.

## Nothing Running Is Not The Same As Nothing Left
A goal whose tasks are all still queued does not stop complete; those tasks are
listed in `Unfinished`, and `Open` holds only running work.

## Nothing Moving Is A Stop
`StopStalled` ends a run when no task changed state for the stalled bound of
turns, naming the stall in `Why`.

## A Run That Comes To Rest Without Landing Is Not Shipped
- A goal whose tasks are all at rest, one of them abandoned, stops complete.
- `Shipped` is false and the abandoned task is in `Unlanded`.
- `Unfinished` and `Open` are empty.

## A Dropped Task Does Not Stop A Goal Shipping
A goal whose other tasks landed and one task was dropped stops complete with
`Shipped` true and `Unlanded` empty.

## A Gate That Refused Is Held On The Run
- Work a turn found ready to close and a gate refused is in `Blocked`, naming the gate.
- `Blocked` keeps every refusal for the whole run.
- Held work is not in `Closed`.

## A Run Judges A Question In A Person's Place, Up To A Bound
- Questions the pass could not answer go to the judge with the goal's settled work.
- With no bounds given a run gets 20 turns, 5 judgements, 200,000 tokens and 3 stalled turns.
- Each judgement spends from the judgement bound and the token bound, by what the turn reported.
- A question past either bound is not judged; it is listed in `Unanswered`.
- A judge that could not run is a `StopFailed`, not an answer.

## A Refusal Is An Answer, And It Is Retried Never
- A decline is recorded once and counted against the bound.
- Nothing is delivered for a decline.
- The declined question is not put to the judge again in the run; it stays in `Unanswered`.

## Taste Is Judged From The Same Budget, And Only Once
- After the work's questions, each turn puts `Candidates` cards to `TasteJudge`.
- A taste judgement spends from the same judgement bound and is recorded the same way, on the work that supplied the newest evidence.
- A decided card is applied once and named in `Promoted`.
- A card is asked at most once per run.

## A Card The Model Declines Is Held And Named
A declined card is not promoted, is named in `Held`, and the ledger gains a
decline and no decision.

## A Workful Turn Spends Its Judgement On The Work Before The Taste
When the work's questions use up the judgement bound, no card is judged that
turn and the work's unanswered question is named in `Unanswered`.

## A Driver Without A Taste Judge Leaves Taste Alone
With no `TasteJudge`, no card is asked, promoted or held.

## A Judgement Is Recorded Before It Is Delivered
The judgement is recorded on the work before the answer is sent to the executor.

## A Run Reports What A Person Is Still Needed For
- `Answered` lists the questions the run settled.
- `Unanswered` lists every question it did not settle, each with its work.
- `Open` lists tasks still open; `Closed` lists tasks that came to rest during the run, read from the ledger.
- `Unanswered` is empty only when the goal shipped.

## A Driver With No Judge Stops At The First Question
With no judge, the run spends no judgement and stops at the first question,
naming it in `Unanswered`.
