# Ledger

## New Work Starts Queued
`add` returns work in state `queued` with a `state` event recording it.

## Only Listed Transitions Are Allowed
queued→briefed→running→(needs-input|review)→soft-done→done, with blocked and dropped reachable from open states; anything else throws IllegalTransition naming both states.

## Soft Done Requires A Done Report And A Passing Verify
From `review`, `softDone` refuses (NotReady listing what is missing) until the latest report starts with DONE and the latest verify starts with pass.

## Code Changes Need A Pull Request Before Soft Done
With `codeChanged` true, `softDone` also requires a `pr` event.

## Feedback Seen Twice Becomes A Distill Candidate
`distill` returns groups keyed by card, else project, else global, only where two or more feedback entries share the key.
