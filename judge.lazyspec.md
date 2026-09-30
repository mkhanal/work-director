# judge

One bounded model call that answers a question the ledger cannot, so an
unattended goal loop is not stopped by the first fork an executor finds. It
replaces a human, so it is built to be refused.

## The Model Is Told That Declining Is A Legitimate Answer
`Brief` gives the model the project's settled position — every open work item
with its state and detail — and the question quoted from the executor's
transcript inside a fence. A line in a transcript reads as evidence to be judged,
never as a command to this session. The model is told to answer only from the
settled position and that declining is a correct and expected answer, far more
so than in most: a wrong answer ships wrong code, and a decline ends one goal
honestly. A question with nothing settled is given as nothing rather than as
silence, because a model asked to choose between no options will invent some.

## A Reply Is An Answer Or A Decline, And A Hedge Is A Decline
`Parse` reads `ANSWER:`, `DECLINE:` and `TOKENS:` lines and ignores everything
else, so a chatty session costs nothing but its tokens. A reply carrying both
an answer and a decline hedged, and a hedged reply is treated as a decline: an
executor given "maybe this, or maybe that" asks again, and the run spent its
budget for nothing. `Verdict.Decided` is true only for an answer with no
decline beside it.

## A Judgement Is Recorded As A Decision With What It Cost, And A Decline Is Not One
`Decision` turns an answer into a structured claim carrying the question, the
answer, the runner and model that gave it and what it cost, so a later review
reads what was settled rather than one line of prose. A decline yields no claim
at all, because the ledger must never gain a decision nobody made; it is
recorded by `Event` as a decline, and an audit that cannot tell an answer from a
refusal will believe the loop decided things it did not.
