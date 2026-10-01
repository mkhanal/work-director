# promotion

Finds the rule cards worth asking about as global taste, and writes one when a
judgement decides so. It used to decide on its own, by counting; now it finds,
and the judgement is made elsewhere and recorded like every other judgement.

## A Card With One Observation Is Worth Asking About
`PromotionCandidates` returns every project-scoped card that is already `adopted`,
has no global card yet, and has at least one piece of feedback attached to it —
most evidence first, ties by id so two runs over the same cards ask in the same
order. A card with no evidence is not a candidate: nothing has ever shown it to be
right, so there is nothing to put to a model. One observation is enough to *ask*
about, and whether it is enough to *believe* is the model's judgement. The bar on
asking is deliberately not the bar on promoting: a threshold of two projects or
two attached observations was a proxy for "is this rule really general", and a
proxy that grows on its own ends up in the taste having only ever been seen twice
in the same place.

## A Global Card Is Written At The Status Asked, And Never Overwritten
`WriteGlobal` writes `<id>-global` beside the project card it came from, with the
project card's category, kind and body, `scope: [global]`, the evidence ids, and
the status given. The project card stays as it is and the file is created with
`O_EXCL`, so an existing global card is never overwritten: a taste decision that
could be edited in place would be a decision with no history, and the review
surface shows a change rather than a state. `AdoptCard` is `WriteGlobal` at
`candidate`, and it is the person's own act of proposing — a judgement that
promotes a card does not come through it, because on that path the judgement is
the gate and a hand-typed card is not one.
