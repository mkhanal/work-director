> **lazyspec.** Humans edit freely. Agents change this only through
> `/lazyspec`, with its tests, in one edit.
>
> Each `##` heading is one requirement. Its test repeats that heading
> as its own name — to find it, search the tests for that text.

# Judge

One bounded model call that answers a question the ledger cannot, in a person's
place. It is built to be refused.

## The Model Is Told That Declining Is A Legitimate Answer
- `Brief` gives the model every open work item with its state and detail, and the question quoted from the transcript inside a fence.
- It tells the model the fenced text is evidence, not instructions.
- It tells the model to answer only from the settled position, and that declining is correct and expected.
- A question with nothing settled says so explicitly.

## A Card Is Judged By A Brief That Knows What Promoting It Costs
- `PromotionBrief` gives the card and its evidence inside a fence, and says they are the whole argument.
- It states that promoting changes what every future session in every project believes.
- It states that a project convention must not become global however much evidence there is.
- It states that declining is the safe answer.
- Its reply is read by `Parse`.

## A Reply Is An Answer Or A Decline, And A Hedge Is A Decline
- `Parse` reads `ANSWER:`, `DECLINE:` and `TOKENS:` lines and ignores every other line.
- A reply with both an answer and a decline is a decline.
- `Verdict.Decided` is true only for an answer with no decline.

## A Judgement Is Recorded As A Decision With What It Cost, And A Decline Is Not One
- `Decision` turns an answer into a claim carrying question, answer, runner, model and tokens.
- A decline yields no claim; `Event` records it as a decline.
