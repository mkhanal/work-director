# review

The periodic audit. A pass is a diff, not a dump: it answers what the loop
decided since the last acknowledgement, what it cost, when each claim took
hold, what has been undone, which taste cards it ran under, and what stopped
without shipping.

## A Pass Is A Diff From The Cursor
`From` reads the two cursors — the event stream and the feedback stream, kept
apart because they have separate ids and a pass that acknowledged one and not
the other would re-show decisions or swallow a new card. A cursor that does not
exist yet reads as 0, so the first pass sees everything rather than nothing.
`--since` and `--since-feedback` override the window without moving anything.
`Empty` distinguishes "nothing was read" from "nothing happened": both are
honest, and only the second means the loop is idle.

## Reading A Pass And Having Seen It Are Different Acts
`Acknowledge` moves both cursors to the end of the pass, and `From` never moves
them. A pass that fails to print, or a reader that crashes mid-read, gets the
same pass again rather than skipping what it never saw. A cursor refuses to move
backwards, because a sweep that forgets is a sweep that shows the same thing
twice and hides what changed since.

## Recorded Time And Effective Time Are Separate Fields
An event carries when the ledger learned of it and, when the two genuinely
differ, when it became the thing in force. A model that answered a question in
one session and whose answer settled in a later one has two dates, and a review
needs to know which date a claim is judged against. An event whose two moments
are the same stores no effective time, so a reader can tell "not separated" from
"recorded the same way". A decision made in one session and effective later is
the case this exists for.

## A Decision Carries Its Parts
A structured decision holds the question it settled, the answer, who decided,
which runner and model did it, what it cost in tokens, and what it reverses. A
claim with no question behind it cannot be reviewed, so both question and answer
are required; everything else is optional because a person types one line and a
model fills in more, and a missing field says exactly that. A decision recorded
as one line of prose has no payload at all and is still reviewable from its body.

## A Reversal Is A New Decision, Not An Edit
`Reverse` records that a decision no longer stands as a fresh decision naming
the one it undoes, and marks the original with what reversed it. The original
claim, its body and the moment it was recorded are left as they were made, so a
reader sees the claim and the correction in the order they happened. Editing the
original would lose the claim entirely, which is the one thing an audit cannot
do. The reason is required: a reversal nobody can read the motive for is
indistinguishable from an error. Only a decision can be reversed.

## A Pass Carries The Taste It Ran Under
Feedback that promoted a card since the cursor ships with the pass, because a
card that changed what the loop believes is part of what the loop decided: the
claims under it were made by a taste the reader has not seen. Feedback with no
card is an observation and is left for `wd distill` rather than shown here.

## Work That Stopped Without Shipping Is In The Same Pass
An abandon event appears in the pass with its reason, so a review reads what
failed to land alongside what was decided. A claim whose work has since left the
ledger is still listed: reviewing the audit trail must not fall over because the
thing it was about is no longer there.

## A Pass Says How Each Change Landed
A landing appears in the pass with which kind of place it reached — a commit
already in the product, or a pull request waiting on a merge. Both satisfy the
gate, because the gate asks whether the work landed and not how many people
looked at it on the way, but they are different facts: an audit that cannot tell
them apart cannot say whether a change was ever reviewed at all, and that is the
question a pass about a loop that acts in a person's place most needs answering.
The kind is recorded when the link is filed, by the one thing that knows it, and
never inferred from the shape of a url — a landing whose kind is not on the
record is shown as unknown rather than guessed at. A landing whose work has
since left the ledger is still listed, with the link and the kind it did have.
