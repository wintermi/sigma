# Internal behavioral evaluations

`internal/evals` contains Sigma's repository-only framework for behavioral,
model-backed checks. It provides generic harness and judge contracts, a Sigma
text harness with optional caller-owned tool execution, paired
baseline/candidate summaries, and private JSONL artifacts. It is not part of
Sigma's public Go API.

## Run the live smoke suite

The opt-in runner is `cmd/sigma-evals-runner`:

```sh
mise run eval -- -provider openai -model gpt-5.6-sol
```

`SIGMA_EVAL_PROVIDER` and `SIGMA_EVAL_MODEL` provide equivalent defaults.
Command-line values take precedence and must be supplied together. The bundled
runner supports direct OpenAI Responses, OpenCode Go, both Fireworks text
surfaces, and native Vertex Gemini models. Each provider is registered
explicitly by the suite. Its six provider-neutral cases cover factual recall,
arithmetic, exact formatting, JSON extraction, multi-turn recall, and a local
tool-call round trip. Selected models must support tools.

The selected provider and model are the baseline. Repeat `-candidate` with a
`provider/model` reference to compare explicit catalog models, add
`-repetitions` for paired samples, and use a Go-style `-run` expression to
filter stable case names:

```sh
mise run eval -- \
  -provider openai \
  -model gpt-5.6-sol \
  -candidate opencode-go/kimi-k3 \
  -repetitions 3 \
  -run 'factual-recall|multi-turn-recall'
```

Single-model smoke scores are hard failures. Comparative scores are
observational and feed the paired report; setup, execution, judge, telemetry,
timeout, and persistence errors still fail the command.

The task runs that command directly and forwards command arguments, including
`-timeout`. Each case, harness, and repetition receives an independent
`-case-timeout`, defaulting to one minute and bounded by the overall timeout,
so one stalled provider run does not cancel later evaluations. Set
`-case-timeout 0` to use only the overall timeout. Live evaluations are not
part of `mise run go:test` or `mise run ci`.

## Write a Sigma harness

Create and register the provider normally, then pass the explicit client and
model into the harness:

```go
harness, err := evals.NewSigmaTextHarness(evals.SigmaHarnessConfig{
	Name:   "candidate-prompt",
	Client: client,
	Model:  model,
	BaseRequest: sigma.Request{
		SystemPrompt: "Answer directly.",
	},
})
```

`evals.Prompt` runs one turn. `evals.Conversation` runs a sequence of prompts,
replaying each successful assistant response into the next request. The
harness does not execute tools by default; an assistant tool-call stop remains
an evaluation error unless `SigmaHarnessConfig.ToolExecutor` is set.
Caller-owned executors return text through `SigmaToolOutput`, may mark a result
as an intentional tool error for model recovery, and may return a Go error for
an operational failure. Successful and error results are replayed with their
tool name and recorded in normalized events and the private transcript.
`MaxToolRounds` bounds tool-call continuations per prompt, defaults to four when
an executor is present, and must not be set without an executor.

For domain-specific assertions, use `evals.NewSigmaHarness` with an output
function. The function receives the final response, final assistant message,
and complete replayable conversation.

## Run and compare harnesses

`evals.Run` integrates with `testing.T` and records the run during `t.Cleanup`.
Call `Runner.Close` from `TestMain` after `m.Run` so all cleanups have completed.
Use one `evals.Run` call per Go test when final test status must be attributed
to an individual run.

Every `evals.Case` requires a stable, nonblank `ID`. Give baseline and candidate
runs of the same scenario the same ID, even when their Go subtest names differ.
Use distinct IDs for distinct scenarios that happen to share input. Pairing uses
the evaluation set, source file, case ID, input identity, and repetition; full
Go test names remain diagnostic labels.

An optional `Case.Validate` callback receives the context and `JudgmentInput`.
It runs after successful harness execution and JSON validation, before judges.
Return an error for operational invariants such as missing required telemetry:
the run then fails, records the error and output, skips judges, and is excluded
from paired metrics. Use judges for correctness and thresholds for score-based
test failures. Validator and judge inputs are read-only by convention.

`evals.NewHarnessTable` emits baseline and candidate rows in repetition order.
Inputs implementing `EvalGroupID() string` use that stable identity for pairing;
other inputs use a hash of deterministic JSON. Judges return finite numeric
scores. A score of at least `1` is a pass. A nil judge threshold records results
without failing the test, which is the normal mode for comparative suites.

Aggregate input, output, and total token counts are `*int`: nil means unavailable
or incomplete; a pointer to zero is a measured zero. Estimated cost is optional
independently of tokens. Sigma harnesses publish aggregates only when every
model turn, including tool continuations, supplied the measurement. Once a turn
is missing, later measurements cannot make that aggregate complete. Partial
per-turn usage remains in transcripts. Generic comparisons can score an answer
while omitting unavailable efficiency metrics; the smoke suite requires complete,
positive total tokens and treats missing usage as an operational failure.

## Artifacts

Each invocation writes private-mode files beneath ignored `.eval/` storage by
default. `SIGMA_EVAL_ARTIFACT_DIR` or the runner's `-artifact-dir` flag selects
an exact directory. `runs.jsonl` indexes each run; transcripts, sources, and
other attachments are isolated under a hash of the run ID.

Artifacts include complete prompts, responses, tool traces, caller attachments,
and usage data. They may contain sensitive content. Review them before sharing,
and never commit them.

New `runs.jsonl` records use `schemaVersion: 2` and include `caseId`, `evalSet`,
immutable JSON snapshots of `input` and `output`, and judgments with their
configured `name`, score, and reason. Token and cost fields use JSON null for
unavailable measurements. Unserializable inputs fail before dispatch;
unserializable results fail the run while retaining other valid evidence.
Comparison reports also use schema version 2 and include case IDs in diagnostics.
Iteration metadata retains schema version 1. Existing artifacts are not migrated
or rewritten; an explicitly reused directory can contain records of both versions.

Sigma transcripts start with a configuration record after the system-prompt
transform succeeds. It records model/provider/API identity, the transformed
system prompt, tool definitions, and the effective maximum tool rounds. Each
completion also captures allowlisted controls at
`merged-options-before-request-adjustments`: merged client/model/call sampling
and output limits, reasoning, tool choice, structured output, transport/cache,
and timeout/retry settings. Durations are recorded in nanoseconds. Option
functions execute normally once per request; capture does not invoke them again.

These control snapshots precede automatic request adjustments, provider-neutral
mappings, provider extensions, and authentication defaults. They are not final
wire payloads. Credentials, headers, clients, resolvers, callbacks, metadata,
and arbitrary provider-option maps are excluded from control capture. Prompts,
tools, caller inputs, and outputs remain complete private evaluation content.
