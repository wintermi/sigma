# Internal behavioral evaluations

`internal/evals` contains Sigma's repository-only framework for behavioral,
model-backed checks. It provides generic harness and judge contracts, a Sigma
text harness, paired baseline/candidate summaries, and private JSONL artifacts.
It is not part of Sigma's public Go API.

## Run the live smoke suite

The opt-in runner is `cmd/sigma-evals-runner`:

```sh
mise run eval -- -provider openai -model gpt-5.6-sol
```

`SIGMA_EVAL_PROVIDER` and `SIGMA_EVAL_MODEL` provide equivalent defaults.
Command-line values take precedence and must be supplied together. The bundled
runner supports direct OpenAI Responses, OpenCode Go, both Fireworks text
surfaces, and native Vertex Gemini models. Each provider is registered
explicitly by the suite. Its provider-neutral cases cover factual recall,
arithmetic, exact formatting, JSON extraction, and multi-turn recall.

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
harness does not execute tools; an assistant tool-call stop is an evaluation
error that remains visible in the recorded trace.

For domain-specific assertions, use `evals.NewSigmaHarness` with an output
function. The function receives the final response, final assistant message,
and complete replayable conversation.

## Run and compare harnesses

`evals.Run` integrates with `testing.T` and records the run during `t.Cleanup`.
Call `Runner.Close` from `TestMain` after `m.Run` so all cleanups have completed.
Use one `evals.Run` call per Go test when final test status must be attributed
to an individual run.

`evals.NewHarnessTable` emits baseline and candidate rows in repetition order.
Inputs implementing `EvalGroupID() string` use that stable identity for pairing;
other inputs use a hash of deterministic JSON. Judges return finite numeric
scores. A score of at least `1` is a pass. A nil judge threshold records results
without failing the test, which is the normal mode for comparative suites.

## Artifacts

Each invocation writes private-mode files beneath ignored `.eval/` storage by
default. `SIGMA_EVAL_ARTIFACT_DIR` or the runner's `-artifact-dir` flag selects
an exact directory. `runs.jsonl` indexes each run; transcripts, sources, and
other attachments are isolated under a hash of the run ID.

Artifacts include complete prompts, responses, tool traces, caller attachments,
and usage data. They may contain sensitive content. Review them before sharing,
and never commit them.
