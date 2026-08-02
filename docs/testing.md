# Testing

Sigma tests must be deterministic by default. Do not introduce live LLM calls in
unit tests, examples, or `mise run go:test`.

## Faux Providers

Use `sigmatest` for client-level tests:

```go
provider := sigmatest.NewFauxProvider(sigmatest.Script{
	Final: sigma.AssistantMessage{
		Content: []sigma.ContentBlock{sigma.Text("ok")},
	},
})
registry, err := sigmatest.Registry(provider)
if err != nil {
	t.Fatal(err)
}

client := sigma.NewClient(sigma.WithRegistry(registry))
text, err := client.CompleteText(context.Background(), sigmatest.TextModel(), "hello")
```

`FauxProvider` records request captures so tests can assert behavior without
testing provider transports:

```go
request, ok := provider.LastRequest()
if !ok {
	t.Fatal("expected request")
}
```

Use `sigmatest.NewFauxImageProvider` and `sigmatest.ImageRegistry` for image
generation tests.

## Provider Adapter Tests

Provider package tests should use `httptest.Server`, fake provider clients, or
checked-in fixtures. They should assert behavior that would catch real bugs:

- request payload shape for branching logic
- auth precedence and credential errors
- streaming event translation
- partial tool-call JSON handling
- cancellation behavior
- provider error mapping and redaction
- retry boundaries before stream body consumption

Avoid tests that mirror static struct defaults or simply restate generated model
metadata field-by-field.

## Golden Files

Golden files are useful for provider wire payloads when the payload is a
contract. Keep them deterministic, reviewable, and free of credentials. The
helper in `internal/goldentest` is available for JSON golden comparisons.

## Live Tests

Live tests must be opt-in behind explicit environment variables and skipped by
default. They are not required for ordinary verification and should not run in
`mise run go:test` unless a maintainer intentionally enables them.

## Behavioral Evaluations

Sigma's repository-internal evaluation framework lives in `internal/evals`.
It provides generic harnesses and judges, a sequential Sigma text harness,
paired baseline/candidate summaries, and private JSONL artifacts without adding
a public evaluation API or an agent/tool execution runtime.

Run the provider-backed factual smoke suite from the repository root:

```sh
mise run eval -- -provider openai -model gpt-5.6-sol
```

The task runs `cmd/sigma-evals-runner` with `go run` and forwards command
arguments. It is not included in `mise run go:test` or `mise run ci`.

`SIGMA_EVAL_PROVIDER` and `SIGMA_EVAL_MODEL` are equivalent defaults. Provider
registration remains explicit in each suite. The runner does not infer or
auto-register adapters from catalog metadata.

The bundled suite supports `openai`, `opencode-go`, `fireworks`,
`fireworks-anthropic`, and native `google-vertex` catalog models. OpenCode Go
uses `OPENCODE_API_KEY`; both Fireworks routes use `FIREWORKS_API_KEY`. Native
Vertex requires project and location environment values plus an externally
supplied access token or API key. See `cmd/sigma-evals-runner/README.md` for the
complete environment names and examples.

Each invocation sequentially checks factual recall, arithmetic, exact
formatting, JSON extraction, and multi-turn recall. Every case has a local
deterministic judge and its own run artifact, and independent failures do not
prevent later cases from running.

The command prints each case's pass/fail status, score, token counts, latency,
estimated cost when available, bounded final output, and an invocation summary.
Use `-run` with a Go regular expression to select case names.

The selected `-provider` and `-model` form the baseline. Repeat
`-candidate provider/model` to compare one or more explicitly selected catalog
models, and use `-repetitions` to set the positive paired sample count:

```sh
mise run eval -- \
  -provider openai \
  -model gpt-5.6-sol \
  -candidate opencode-go/kimi-k3 \
  -repetitions 3 \
  -run 'factual-recall|multi-turn-recall'
```

Candidate scores are observational: an incorrect answer remains visible in the
paired pass-rate report but does not alone fail a comparative invocation.
Configuration, model execution, judging, telemetry validation, timeout, and
artifact failures remain fatal. Full responses and transcripts remain in the
private artifact directory.

Evaluation artifacts are stored under an ignored `.eval/` invocation directory
unless `-artifact-dir` or `SIGMA_EVAL_ARTIFACT_DIR` selects an exact path. They
use private filesystem permissions but contain complete prompts, responses,
tool traces, source attachments, and usage data; review them before sharing.

The live runner is outside `mise run go:test` and `mise run ci`. Framework,
artifact, comparison, cancellation, and transcript behavior is covered with
deterministic faux providers in the ordinary test suite.

## Parallelism

Use `t.Parallel()` when a test only touches local state, `t.TempDir()`, local
buffers, or isolated registries. Do not combine `t.Parallel()` with
`t.Setenv()`, package-global mutation, shared ports, or order-dependent side
effects.

## Verification

Run:

```sh
mise run go:test
```

Docs are covered by a deterministic internal-link test. External links are not
checked so the default test suite stays offline.
