# Reasoning

Sigma uses "thinking" for provider-neutral reasoning content and exposes
provider-neutral request controls. Provider APIs name this feature differently:
OpenAI calls parts of it reasoning, Anthropic and Bedrock expose thinking, and
Google exposes thinking configuration and thought parts.

## Model Metadata

Check model support before setting reasoning options:

```go
if model.SupportsReasoning() && model.SupportsThinkingLevel(sigma.ThinkingLevelMedium) {
	text, err := client.CompleteText(
		ctx,
		model,
		"Think carefully, then answer briefly.",
		sigma.WithReasoningLevel(sigma.ThinkingLevelMedium),
	)
	_ = text
	_ = err
}
```

`Model.ProviderThinkingLevel` maps a provider-neutral level to the value the
provider expects when `ThinkingLevelMap` is present.
On models with `SupportsThinking`, levels through `high` are supported and use
the level text unless the map overrides them, while `xhigh` and `max` require a
map entry, including on models with no map. `UnsupportedThinkingLevels`
rejects a level.

## Request Options

Use these root options first:

- `sigma.WithReasoningLevel(level)`
- `sigma.WithThinkingBudgetTokens(tokens)`

Use provider-specific option structs when the provider exposes a stable control
that is not provider-neutral:

- `sigma.WithOpenAIOptions(sigma.OpenAIOptions{ReasoningEffort: ...})`
- `sigma.WithAnthropicOptions(sigma.AnthropicOptions{ThinkingBudgetTokens: ...})`
- `sigma.WithGoogleOptions(sigma.GoogleOptions{ThinkingBudgetTokens: ...})`

Provider packages translate supported controls to their wire formats. OpenAI
Responses, Azure, and Codex requests use effort only and ignore
`WithThinkingBudgetTokens`. An explicit `ThinkingLevelOff` on a reasoning model
sends the model's mapped off effort, or `none` on Responses routes other than
GitHub Copilot; Chat Completions `reasoning_effort` routes send only a mapped off
value. Leaving the level unset keeps the provider's default effort; Responses
reasoning models still request `reasoning.encrypted_content` so that reasoning
can be replayed on the next turn. If a model does not advertise the requested
thinking level, `Client.Stream` returns a local invalid-options error before
dispatch. Turning thinking off on a Google or
Vertex Gemini model whose metadata marks `off` unsupported requests the model's
lowest supported level instead, such as `LOW` for Gemini 3.8 Flash or the minimal
budget for Gemini 2.5 Pro. Anthropic and Bedrock Claude adaptive thinking sends
`xhigh` effort only to Opus 4.7 and later, Sonnet 5, Haiku 5, and Fable 5, or
where metadata maps `xhigh` explicitly; other adaptive models receive `high`.
Bedrock adaptive thinking for those same families, outside GovCloud, also sends
`thinking.block_binding` with `drop_block` and the matching beta, so replayed
signed thinking is dropped rather than rejected after the system prompt or
tools change.

## Streaming Thinking

Thinking can stream independently from text:

```go
thinking := map[int]string{}

for event := range stream.Events() {
	if event.Kind != sigma.EventKindThinkingDelta || event.ContentIndex == nil {
		continue
	}
	thinking[*event.ContentIndex] += event.DeltaText
}
```

Final assistant messages store thinking as `ContentBlockThinking`. The block may
carry text, an opaque provider signature, or redaction metadata. Persist opaque
signatures if the provider needs them for continuation, but treat them as
sensitive application data.

When history is replayed to a different provider, API, or model, thinking blocks
from assistant messages whose recorded provenance differs from the target are
dropped; their text and tool calls are kept. Chat Completions routes that require
`reasoning_content` on assistant messages send it empty for those turns. Messages
without provenance are treated as the target model's own. The opt-in
`sigma.TransformRequestForModel` helpers still convert thinking to text when
called explicitly.

## Display Policy

Sigma does not decide whether thinking should be visible to users. Many
applications hide streamed thinking and only use it for provider continuity,
debugging, or audit views. If you show thinking, keep it separate from final
answer text and make sure persistence rules match your product policy.

## Provider Status

Reasoning support is provider- and model-specific:

- OpenAI Responses has fixture-tested reasoning paths.
- OpenAI Chat Completions-compatible endpoints vary; use
  `OpenAICompletionsCompat` for routers and local models.
- Fireworks Chat Completions maps reasoning levels to `reasoning_effort` and
  thinking budgets to Fireworks' `thinking` object.
- Anthropic, Google, Bedrock, and Mistral have fixture-tested thinking paths.
- Mistral Conversations maps adjustable-reasoning models to
  `completion_args.reasoning_effort` and native Magistral models to
  top-level `prompt_mode`.

See [provider parity](provider-parity.md) for current status.
