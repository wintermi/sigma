# Sigma evals runner

`sigma-evals-runner` is an opt-in live evaluation command. The suite runs five
provider-neutral smoke cases against a caller-selected supported catalog model:
factual recall, arithmetic, exact formatting, JSON extraction, and multi-turn
recall. It is intentionally outside the deterministic test and CI tasks.

Cases execute sequentially, receive independent pass/fail judgments, and write
separate run records and transcripts. The command continues through independent
case failures so one invocation reports the complete smoke result.

Successful and failed cases print a compact result line with the judgment,
score, input/output tokens, latency, estimated cost when available, and a
bounded copy of the final output:

```text
PASS factual-recall score=1.00 tokens=18(in=12,out=6) latency=842ms cost=$0.000031 output="Paris"
```

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

Command-line model selection takes precedence and requires both values. Use
`-artifact-dir` or `SIGMA_EVAL_ARTIFACT_DIR` to select an exact output
directory; otherwise each invocation creates a private directory beneath
`.eval/`.

Artifacts contain complete prompts, responses, model traces, and usage data.
They may contain sensitive content and must not be committed or shared without
review.
