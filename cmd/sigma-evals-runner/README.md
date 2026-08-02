# Sigma evals runner

`sigma-evals-runner` is an opt-in live evaluation command. The suite runs five
provider-neutral smoke cases against a caller-selected supported catalog model
or a baseline/candidate model table: factual recall, arithmetic, exact
formatting, JSON extraction, and multi-turn recall. It is intentionally outside
the deterministic test and CI tasks.

Cases execute sequentially, receive independent pass/fail judgments, and write
separate run records and transcripts. The command continues through independent
case failures so one invocation reports the complete smoke result.

Successful and failed runs print a compact result line with the model role,
full model identity, case, repetition, judgment, score, input/output tokens,
latency, estimated cost when available, and a bounded copy of the final output:

```text
PASS role=baseline harness=openai/gpt-5.6-sol case=factual-recall repetition=1 score=1.00 tokens=18(in=12,out=6) latency=842ms cost=$0.000031 output="Paris"
```

The command ends with correctness and operational-failure totals. Comparative
runs also print paired pass-rate lift and candidate-minus-baseline token,
latency, and estimated-cost deltas.

Run the command from the repository root:

```sh
mise run eval -- -provider openai -model gpt-5.6-sol
```

Supported provider IDs and credentials are:

| Provider ID | Route | Required environment |
| --- | --- | --- |
| `openai` | Direct OpenAI Responses | `OPENAI_API_KEY` |
| `opencode-go` | OpenCode Go model-selected API | `OPENCODE_API_KEY` |
| `fireworks` | Fireworks Chat Completions | `FIREWORKS_API_KEY` |
| `fireworks-anthropic` | Fireworks Anthropic Messages | `FIREWORKS_API_KEY` |
| `google-vertex` | Native Vertex Gemini | Project, location, and an API key or access token |

For example:

```sh
mise run eval -- -provider opencode-go -model kimi-k3
mise run eval -- -provider fireworks -model accounts/fireworks/models/kimi-k3
```

The selected `-provider` and `-model` form the baseline. Add one or more
`-candidate provider/model` flags and a positive `-repetitions` count to run a
paired comparison. Candidate references split at the first slash, so provider
model IDs may contain additional slashes:

```sh
mise run eval -- \
  -provider openai \
  -model gpt-5.6-sol \
  -candidate opencode-go/kimi-k3 \
  -candidate fireworks/accounts/fireworks/models/kimi-k3 \
  -repetitions 3
```

Use `-run` with a Go regular expression to select stable case names:

```sh
mise run eval -- -provider openai -model gpt-5.6-sol -run 'arithmetic|json-extraction'
```

Candidate correctness is observational: a low score is included in comparison
statistics without failing the process. Setup, execution, judge, telemetry,
timeout, and artifact failures still return a non-zero status. A single-model
run retains hard correctness thresholds.

Native Vertex requires `GOOGLE_CLOUD_PROJECT` or `GCLOUD_PROJECT`,
`GOOGLE_CLOUD_LOCATION` or `GOOGLE_CLOUD_REGION`, and one of
`GOOGLE_CLOUD_ACCESS_TOKEN`, `GOOGLE_CLOUD_API_KEY`, or `GOOGLE_API_KEY`:

```sh
mise run eval -- -provider google-vertex -model gemini-2.5-flash
```

Additional arguments are forwarded to `sigma-evals-runner`. The same command
can also be run through the generic Go task:

```sh
mise run go:run -- ./cmd/sigma-evals-runner -provider openai -model gpt-5.6-sol
```

Use `-timeout` to change the five-minute overall command deadline.

The equivalent environment defaults are:

```sh
SIGMA_EVAL_PROVIDER=openai \
SIGMA_EVAL_MODEL=gpt-5.6-sol \
mise run eval
```

Command-line baseline selection takes precedence and requires both values.
Candidate models are selected explicitly on the command line. Use
`-artifact-dir` or `SIGMA_EVAL_ARTIFACT_DIR` to select an exact output
directory; otherwise each invocation creates a private directory beneath
`.eval/`.

Artifacts contain complete prompts, responses, model traces, and usage data.
They may contain sensitive content and must not be committed or shared without
review.
