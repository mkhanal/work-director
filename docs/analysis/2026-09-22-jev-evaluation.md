# Jev evaluation — 22 September 2026

Jev looks worth trying for executor-question routing. It matched our labels on all 12 routing examples, but this was a small synthetic test. We have not measured whether it saves tokens, time, or money compared with the director. This note records the first run and prepares a second, harder one with comparison models (see [Version 2](#version-2-prepared-not-yet-run)); it does not add an integration.

## What we tested

We called `typesafe-ai/jev` through Vercel AI Gateway's evaluation endpoint for two tasks: tagging engineering feedback and deciding where an executor question belongs. The [fixtures and rubrics](2026-09-22-jev-fixtures.json) contain 12 examples per task. Expected labels and rubrics were fixed before the run. Each case was submitted twice, sequentially, for 48 successful calls.

The labels are the author's judgments. They were not independently reviewed, and some feedback categories overlap. These are synthetic examples written for this experiment, not a held-out sample of real director work.

Routing covered technical facts, routine engineering choices, business decisions, merge approval, mixed questions, and missing context. Two cases across the dataset included instructions to override the classification policy. Both matched their expected labels; that is too little evidence to claim general resistance to prompt injection.

## Results

| Measure | Observed result |
| --- | --- |
| Question routing | 12/12 unique cases; 24/24 calls matched expected labels |
| Feedback tagging | 11/12 unique cases; 22/24 calls matched expected labels |
| Same choice on both repetitions | 24/24 cases |
| Successful-attempt latency, median / p95 | 392 / 615 ms |
| Input / output tokens | 25,856 / 3,596 |
| Gateway market cost | $0.001086 for 48 successful calls |
| Gateway reported cost | $0 for those calls |
| Accepted at chosen-option probability ≥ 0.99 | 28/48 calls (58%), 26 correct; both mistakes still accepted |

Latency includes network time but excludes pacing and rate-limit backoff. The gateway's own provider timing puts the median at 237 ms; the rest is network and gateway overhead. Three HTTP 429 responses were observed during the run. The stored results contain one of them; the other two were retried inside the runner and appear only in console output that was not kept, so that count cannot be checked from the artifacts. Requests resumed with pacing and backoff, so the latency figures do not describe total elapsed time or sustained throughput. Cost and token totals exclude two separate connectivity probes. Gateway cost metadata is not a billing statement, and the reported $0 is not a promise of free usage. The market cost equals the listed price: $0.042 per million input tokens, with output not charged.

The one mismatch happened twice:

> The runtime parser belongs at the incoming request, not in each internal helper.

We expected `types_and_schemas`; Jev chose `organisation`, with a choice probability of 0.99 and provider confidence of 0.98. The sentence concerns both placement and boundary validation, which exposes overlap in the taxonomy. Under our frozen labels it is a mistake, and a high-confidence cutoff would have accepted it. Agreement across two repetitions does not establish accuracy or calibrated confidence.

## Version 2: prepared, not yet run

Version 1 left four questions open. Version 2 is built to answer them. Its fixtures are committed before any request is sent, so the history shows the labels were not adjusted after seeing results.

| Open question after version 1 | What version 2 changes |
| --- | --- |
| Were the examples too easy? | The [version 2 fixtures](2026-09-22-jev-fixtures-v2.json) keep all 24 version 1 cases unchanged and add 26 boundary cases. They cover overlapping feedback categories, business facts that look like research (contracts, hosting regions), irreversible actions (force-pushing a shared branch, migrating production), a fact paired with an engineering choice, and injected "[SYSTEM]" overrides. |
| Is a mismatch a model error or a debatable label? | Each new case may list `acceptable` labels that a careful reviewer could defend. Results are scored strictly, against `expected` only, and leniently. The version 1 cases have no acceptable labels, so their scores stay comparable. |
| Is Jev better or cheaper than the alternatives? | The same requests go to `anthropic/claude-haiku-4.5`, `openai/gpt-5.6-luna` and `google/gemini-3.5-flash-lite`, the example models in the AI SDK evaluation documentation, and to `anthropic/claude-opus-5` as a stand-in for the director. Those models return no choice probabilities, so the confidence analysis applies to Jev only. |
| How many requests fail or are throttled, and how long does a caller really wait? | Every attempt is logged, including 429 and 5xx responses, with the response time of the successful attempt and the total wait including retries. |

It is not yet confirmed that the gateway's evaluation endpoint serves language models; the gateway lists only Jev as an evaluation model, although it lists specification v4 for the others. If it refuses them, the runner stops each one after three requests, and the comparison needs the AI SDK's own adapters instead.

The rubric is unchanged from version 1, so any change in results comes from the cases and models, not from different wording. Rewriting the feedback taxonomy to separate placement from boundary validation is a separate experiment.

List prices on 22 September 2026, per million input / output tokens: Jev $0.042 / $0, Gemini 3.5 Flash-Lite $0.30 / $2.50, GPT-5.6 Luna $0.20 / $1.20, Claude Haiku 4.5 $1 / $5, Claude Opus 5 $5 / $25. At about 550 input and at most 150 output tokens per call (Jev averaged 75), the full run of 500 calls should cost under $1, most of it Opus.

### Running it

`scripts/jev-evaluation.ts` sends the requests and analyses the results. It has no dependencies and its tests run in CI.

```text
bun scripts/jev-evaluation.ts run docs/analysis/2026-09-22-jev-fixtures-v2.json ~/.work-director/evaluations/jev-v2
bun scripts/jev-evaluation.ts analyze docs/analysis/2026-09-22-jev-fixtures-v2.json ~/.work-director/evaluations/jev-v2
```

The key comes from `AI_GATEWAY_API_KEY` or `~/.work-director/secrets/ai-gateway-key` and is never written to the output directory. Raw results stay private in `~/.work-director`, as in version 1. The runner:

- sends each case twice, in file order, as `{"state":{"text":…},"questions":{"decision":…}}` to `POST https://ai-gateway.vercel.sh/v4/ai/evaluation-model` with headers `ai-gateway-protocol-version: 0.0.1`, `ai-evaluation-model-specification-version: 4` and `ai-model-id`;
- runs models in parallel, pacing each at 3.2 s, the pace that avoided throttling in version 1;
- retries 429, 5xx and network failures up to four attempts, following `Retry-After` within 1 to 60 s; any other rejection is final;
- stops a model after three failed cases in a row, so a model the endpoint does not support costs three requests;
- appends every attempt and result to `attempts.jsonl` and `results.jsonl`, so an interrupted run resumes where it stopped, and refuses to mix results from a different fixtures file (checked by SHA-256);
- writes `summary.json` and prints a comparison table with strict and lenient accuracy per task, agreement between repetitions, latency, and gateway market cost. For Jev it also reports what share of answers clears 0.9 and 0.99 choice probability and how many of those are correct.

The analysis reproduces every version 1 figure above from the original raw responses. The Jev alias is not pinned to an underlying version, so later runs may differ.

The [Vercel Jev model page](https://vercel.com/ai-gateway/models/jev), the [gateway evaluation adapter](https://github.com/vercel/ai/blob/main/packages/gateway/src/gateway-evaluation-model.ts) and the AI SDK [evaluation reference](https://github.com/vercel/ai/blob/main/content/docs/07-reference/01-ai-sdk-core/14-evaluate.mdx) are the vendor references for the model and request interface.

## What we should do next

Run version 2 first. If Jev still matches the cheaper comparison models on the boundary cases, run it alongside the director on real executor questions, showing its routing suggestion without letting it dispatch work. Freeze the rubric, have the director label examples before seeing Jev's answer, and compare disagreements against the current workflow. Measure total latency including retries, token use, and cost for both paths. That comparison should decide whether an integration is worthwhile.

Keep approval, verification, and state-transition gates deterministic. A suggested route cannot authorize a merge or an irreversible action. On API errors or invalid answers, use the existing director workflow; leave mixed and unclear requests with the director. Keep feedback tags advisory until the taxonomy and its boundary cases have had a separate evaluation.

A future integration would need requirements and married tests through `/lazyspec`. The evaluation runner is tooling, outside the areas `lazyspec.md` covers; this experiment changes no director or taste behavior and no requirement headings, and does not justify a global taste card.
