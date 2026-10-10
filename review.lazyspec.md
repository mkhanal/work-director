> **lazyspec.** Humans edit freely. Agents change this only through
> `/lazyspec`, with its tests, in one edit.
>
> Each `##` heading is one requirement. Its test repeats that heading
> as its own name — to find it, search the tests for that text.

# Review

The periodic audit: a diff of what the loop decided since the last
acknowledgement, what it cost, what was undone, which taste it ran under, and
what stopped without shipping.

## A Pass Is A Diff From The Cursor
- `From` reads two cursors, the event stream and the feedback stream, independently.
- A cursor that does not exist reads as 0.
- `--since` and `--since-feedback` set the window without moving a cursor.
- `Empty` is true when the pass found nothing.

## Reading A Pass And Having Seen It Are Different Acts
- `From` never moves a cursor.
- `Acknowledge` moves both cursors to the end of the pass.
- A cursor refuses to move backwards.

## Recorded Time And Effective Time Are Separate Fields
- An event carries `at`, when the ledger recorded it.
- It carries `effective` only when it took hold at a different moment; otherwise `effective` is absent.

## A Decision Carries Its Parts
- A structured decision holds question, answer, who decided, runner, model, tokens, and what it reverses.
- Question and answer are required; every other part is optional.
- A one-line decision has no payload and is reviewed by its body.

## A Reversal Is A New Decision, Not An Edit
- `Reverse` records a new decision naming the one it undoes, and marks the original with what reversed it.
- The original's body and recorded time are unchanged.
- An empty reason is refused.
- Only a decision can be reversed.

## A Pass Carries The Taste It Ran Under
- Feedback that promoted a card since the cursor is in the pass.
- Feedback with no card is not.

## Work That Stopped Without Shipping Is In The Same Pass
- An abandon event is in the pass with its reason.
- A claim or abandon whose work has left the ledger is still listed.

## A Pass Says How Each Change Landed
- A landing is in the pass with its kind: `commit` or `pull-request`.
- A landing recorded without a kind reads as unknown.
- A landing whose work has left the ledger is still listed with its link and kind.
