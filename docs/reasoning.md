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

## Request Options

Use these root options first:

- `sigma.WithReasoningLevel(level)`
- `sigma.WithThinkingBudgetTokens(tokens)`

Use provider-specific option structs when the provider exposes a stable control
that is not provider-neutral:

- `sigma.WithOpenAIOptions(sigma.OpenAIOptions{ReasoningEffort: ...})`
- `sigma.WithAnthropicOptions(sigma.AnthropicOptions{ThinkingBudgetTokens: ...})`
- `sigma.WithGoogleOptions(sigma.GoogleOptions{ThinkingBudgetTokens: ...})`

Provider packages translate supported controls to their wire formats. If a
model does not advertise the requested thinking level, `Client.Stream` returns a
local invalid-options error before dispatch.

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
  `completion_args.prompt_mode`.

See [provider parity](provider-parity.md) for current status.
