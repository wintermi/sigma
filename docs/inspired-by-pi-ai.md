# Inspired by `@earendil-works/pi-ai`

The `sigma` project is inspired by the TypeScript `@earendil-works/pi-ai`
package. This guide maps familiar TypeScript concepts to Go `sigma` patterns
without presenting `sigma` as a replacement path or exact API-name port.

`sigma` keeps the same provider-neutral goal, but uses Go conventions:

- `context.Context` controls cancellation and deadlines instead of
  `AbortSignal`.
- Plain structs and explicit `error` returns replace TypeScript unions and
  thrown exceptions.
- Tool parameters are JSON Schema-compatible values instead of TypeBox schemas.
- Provider implementations are registered through a `Registry`; model metadata
  alone is not enough to make a request runnable.

See also the runnable [examples](../examples/README.md), the
[provider parity matrix](provider-parity.md), and the
[source capability map](source-capability-map.md).

Code snippets are intentionally short comparison fragments unless they include
a `package main` declaration. For complete runnable programs, use the linked
examples.

## Concept Mapping

| TypeScript concept | Go `sigma` equivalent | Notes |
| --- | --- | --- |
| `Context` | `sigma.Request` plus your persisted `[]sigma.Message` | `Request` carries `SystemPrompt`, `Messages`, and `Tools`. Persist messages explicitly, or JSON-serialize `Request` when that matches your app. |
| `Tool` | `sigma.Tool` | `parameters` becomes `InputSchema`. Use `sigma.Schema` or any JSON-marshable object. |
| TypeBox `Type.Object(...)` | JSON Schema-compatible `sigma.Schema` | `ValidateToolCall` supports common JSON Schema keywords, not the full TypeBox runtime. |
| `getProviders()` | `client.Registry().ListProviders()` | Provider entries show text and image API registrations. |
| `getModel(provider, id)` | `sigma.GetModel(provider, id)` or `client.GetModel(provider, id)` | Returns `(sigma.Model, bool)` instead of throwing. |
| `getModels(provider)` | `sigma.Models(filters...)` or `client.Models(filters...)` | The Go API uses filter functions instead of provider-specific overloads. |
| `model.input.includes("image")` | `model.SupportsImages()` or `model.SupportsInput(sigma.ContentBlockImage)` | Models with no explicit input metadata are treated as text-only. |
| `model.reasoning` | `model.SupportsReasoning()` | `ThinkingLevels` and `ThinkingLevelMap` expose finer metadata. |
| `stream(model, context, options)` | `client.Stream(ctx, model, req, opts...)` | Package-level helper is `sigma.StreamModel` because `sigma.Stream` is the stream type. |
| `complete(model, context, options)` | `client.Complete(ctx, model, req, opts...)` | Returns `(sigma.AssistantMessage, error)`. |
| `streamSimple(...)` | `client.Stream(..., sigma.WithReasoningLevel(...))` | Go folds simple reasoning into ordinary options. |
| `completeSimple(...)` | `client.Complete(...)` or `client.CompleteText(...)` with `sigma.WithReasoningLevel(...)` | `CompleteText` is text-only and errors if the assistant produced tool calls or thinking blocks. |
| `validateToolCall(tools, call)` | `sigma.ValidateToolCall(tools, call)` | Returns decoded `map[string]any` and `error`; use `sigma.ToolErrorMessage` for retry messages. |
| `getImageModel(provider, id)` | `sigma.GetImageModel(provider, id)` or `client.GetImageModel(provider, id)` | Returns `(sigma.ImageModel, bool)`. |
| `getImageModels()` | `sigma.ImageModels()` or `client.ImageModels()` | Image models use a separate registry list from text models. |
| `getImageProviders()` | `client.Registry().ListProviders()` | Filter entries with a non-empty `ImageAPI` if you only need image providers. |
| `generateImages(model, request, options)` | `client.GenerateImages(ctx, model, sigma.ImageRequest, opts...)` | Image generation is separate from text streaming/completion. |
| Direct provider functions such as `streamAnthropic` | Provider package registration plus `client.Stream` | Go callers use the root client once a provider adapter is registered. |
| `registerFauxProvider()` | `sigmatest.NewFauxProvider` and `sigmatest.Registry` | Intended for deterministic tests and examples. |
| `baseUrl` on custom model | `sigma.OpenAICompatibleModel` with `BaseURL`, or provider package registration with `openai.WithBaseURL(...)` | Use a custom registry plus `sigma.WithRegistry` when you want isolated local endpoints or routers. |
| `apiKey`, environment lookup | `sigma.WithAPIKey`, `sigma.WithAuthResolver`, `sigma.EnvironmentAuthResolver` | Request API keys override client/default credential resolution. |
| `signal` | `context.Context` | Use `context.WithCancel`, `context.WithTimeout`, or `context.WithDeadline`. |
| `stopReason` strings | `sigma.StopReason...` constants | Go names differ: for example `StopReasonEndTurn`, `StopReasonMaxTokens`, `StopReasonToolCalls`, `StopReasonError`, `StopReasonAborted`. |
| `onPayload`, `onResponse` | No root API-name equivalent | Use provider tests, custom `http.Client`, provider-specific diagnostics, or adapter changes when payload inspection is needed. |

## Provider Coverage

The TypeScript package lists more provider IDs than Go `sigma` currently runs
out of the box. Sigma's Go design is intentionally explicit:

- Built-in text adapters currently cover OpenAI-compatible Chat Completions,
  OpenAI Responses, Azure OpenAI Responses, OpenAI Codex Responses, Anthropic
  Messages, Google Generative AI, Google Vertex, Mistral Conversations, and
  Amazon Bedrock Converse Stream.
- Image generation currently has OpenRouter and OpenAI Images adapters.
- Provider IDs such as DeepSeek, Groq, Cerebras, xAI, Together, GitHub Copilot,
  Fireworks, OpenCode Zen, OpenCode Go, Kimi, MiniMax, Xiaomi, and `custom`
  exist for compatible models, but generated default model coverage and fixture
  coverage vary. OpenCode Zen and OpenCode Go include curated
  OpenAI-compatible built-in metadata, and MiniMax has direct
  Anthropic-compatible registration helpers for the global and CN routes.
- Cloudflare AI Gateway, Cloudflare Workers AI, and non-OpenAI OpenCode routes
  do not have complete Go parity at the time of this guide. Check
  [provider-parity.md](provider-parity.md) before relying on those routes.

## Model Lookup

TypeScript:

```typescript
import { getModel } from "@earendil-works/pi-ai";

const model = getModel("openai", "gpt-4o-mini");
console.log(model.name, model.api);
```

Go:

```go
package main

import (
	"fmt"
	"log"

	"github.com/wintermi/sigma"
)

func main() {
	model, ok := sigma.GetModel(sigma.ProviderOpenAI, "gpt-4o-mini")
	if !ok {
		log.Fatal("model is not registered")
	}
	fmt.Println(model.Name, model.API)
}
```

For runtime calls, make sure the relevant provider package has been registered
on the registry used by your client. Generated model metadata and provider
implementations are separate concerns in Go.

To inspect provider registrations:

```go
for _, provider := range client.Registry().ListProviders() {
	fmt.Println(provider.ID, provider.TextAPI, provider.ImageAPI)
}
```

## Custom OpenAI-Compatible Models

Use an isolated registry when adding local endpoints, provider routers, or
application-specific model metadata:

```go
registry := sigma.NewRegistry()
providerID := sigma.ProviderID("ollama")

_ = openai.Register(registry, providerID)
model := sigma.OpenAICompatibleModel(sigma.OpenAICompatibleModelConfig{
	ID:              "llama3.2",
	Provider:        providerID,
	BaseURL:         "http://localhost:11434/v1",
	ContextWindow:   131072,
	MaxOutputTokens: 8192,
	SupportedInputs: []sigma.ContentBlockType{sigma.ContentBlockText},
})
_ = sigma.RegisterModel(registry, model)

client := sigma.NewClient(sigma.WithRegistry(registry))
```

For vLLM, LM Studio, or custom proxies, keep the same pattern and change the
provider ID, base URL, model ID, headers, and compatibility overrides to match
the endpoint. This does not mutate generated built-in metadata or the package
default registry.

## Streaming

TypeScript `stream()` is an async iterable with a final `result()` method.
In Go, `Client.Stream` returns a `*sigma.Stream`; read events from
`stream.Events()`, then inspect `stream.Err()` and `stream.Final()` or use
`sigma.Collect`.

TypeScript:

```typescript
const s = stream(model, context);
for await (const event of s) {
  if (event.type === "text_delta") process.stdout.write(event.delta);
}
const finalMessage = await s.result();
```

Go:

```go
func streamText(ctx context.Context, client *sigma.Client, model sigma.Model) (sigma.AssistantMessage, error) {
	stream := client.Stream(ctx, model, sigma.Request{
		Messages: []sigma.Message{sigma.UserText("Explain Sigma briefly.")},
	})
	defer stream.Close()

	for event := range stream.Events() {
		switch event.Kind {
		case sigma.EventKindTextDelta:
			fmt.Print(event.DeltaText)
		case sigma.EventKindToolCallDelta:
			if event.PartialToolCall != nil {
				fmt.Print(event.PartialToolCall.ArgumentsDelta)
			}
		}
	}

	if err := stream.Err(); err != nil {
		return sigma.AssistantMessage{}, err
	}
	final, ok := stream.Final()
	if !ok {
		return sigma.AssistantMessage{}, fmt.Errorf("stream ended without final message")
	}
	return final, nil
}
```

Events may be interleaved across text, thinking, and tool-call blocks. Use
`event.ContentIndex` when assembling UI state instead of assuming contiguous
block sequences.

## Completion

TypeScript:

```typescript
const response = await complete(model, {
  messages: [{ role: "user", content: "Write one sentence." }],
});
```

Go:

```go
func complete(ctx context.Context, client *sigma.Client, model sigma.Model) (sigma.AssistantMessage, error) {
	return client.Complete(ctx, model, sigma.Request{
		Messages: []sigma.Message{sigma.UserText("Write one sentence.")},
	})
}
```

For simple prompt/response calls, use `CompleteText`:

```go
text, err := client.CompleteText(ctx, model, "Write one sentence.")
```

`CompleteText` deliberately returns an error if the assistant response contains
non-text content such as thinking blocks or tool calls. Use `Complete` when you
need the full assistant message.

## Tools And Validation

TypeScript tools use TypeBox:

```typescript
const weatherTool: Tool = {
  name: "weather",
  description: "Look up current weather for a city.",
  parameters: Type.Object({
    city: Type.String(),
    units: StringEnum(["celsius", "fahrenheit"]),
  }),
};
```

Go tools use JSON Schema-compatible parameter definitions:

```go
func weatherTool() sigma.Tool {
	return sigma.Tool{
		Name:        "weather",
		Description: "Look up current weather for a city.",
		InputSchema: sigma.Schema{
			"type": "object",
			"properties": map[string]any{
				"city": map[string]any{
					"type": "string",
				},
				"units": map[string]any{
					"type": "string",
					"enum": []any{"celsius", "fahrenheit"},
				},
			},
			"required":             []any{"city"},
			"additionalProperties": false,
		},
	}
}
```

When the model emits a tool call, validate before executing:

```go
args, err := sigma.ValidateToolCall(tools, call)
if err != nil {
	messages = append(messages, sigma.ToolError(call.ID, sigma.ToolErrorMessage(call, err)))
	return messages
}
result, err := runTool(call.Name, args)
if err != nil {
	messages = append(messages, sigma.ToolError(call.ID, err.Error()))
	return messages
}
messages = append(messages, sigma.ToolResult(call.ID, result))
```

`ValidateToolCall` returns a decoded copy of the arguments. It supports common
JSON Schema keywords such as `type`, `properties`, `required`, `enum`, `items`,
`additionalProperties`, numeric bounds, string length bounds, and composed
schemas using `anyOf`, `oneOf`, and `allOf`, plus string `pattern` constraints
and `not`. It does not evaluate every TypeBox or JSON Schema feature;
unsupported keywords such as `$ref`, formats, and conditionals are not enforced.

## Image Input

For chat/complete requests, image input is ordinary message content.

TypeScript:

```typescript
const response = await complete(model, {
  messages: [{
    role: "user",
    content: [
      { type: "text", text: "What is in this image?" },
      { type: "image", data: base64Image, mimeType: "image/png" },
    ],
  }],
});
```

Go:

```go
final, err := client.Complete(ctx, model, sigma.Request{
	Messages: []sigma.Message{
		sigma.UserContent(
			sigma.Text("What is in this image?"),
			sigma.ImageBase64("image/png", base64Image),
		),
	},
})
```

Check capability with:

```go
if model.SupportsImages() {
	fmt.Println("model accepts image content")
}
```

Unlike the source package's historical behavior of silently ignoring images for
some non-vision models, Go callers should check model metadata and the
[provider parity matrix](provider-parity.md) before relying on image input.

## Image Generation

Image generation uses a separate model type and request type.

TypeScript:

```typescript
const model = getImageModel("openrouter", "google/gemini-2.5-flash-image");
const result = await generateImages(model, {
  input: [{ type: "text", text: "Generate a red circle." }],
});
```

Go:

```go
imageModel, ok := sigma.GetImageModel(sigma.ProviderOpenRouter, "google/gemini-2.5-flash-image-preview")
if !ok {
	return fmt.Errorf("image model is not registered")
}

images, err := client.GenerateImages(ctx, imageModel, sigma.ImageRequest{
	Inputs: []sigma.ImageInput{
		sigma.ImageText("Generate a red circle."),
	},
	Size:     string(sigma.ImageSize1024x1024),
	Quality:  string(sigma.ImageQualityLow),
	MIMEType: "image/png",
	Count:    1,
})
if err != nil {
	return err
}
for _, image := range images.Images {
	fmt.Println(image.MIMEType)
}
```

Current provider readiness is intentionally uneven: OpenRouter and OpenAI
Images have runnable adapters, while other image-provider routes may remain
metadata-only. Treat [provider-parity.md](provider-parity.md) as the source of
truth before relying on image workflows.

## Thinking And Reasoning

TypeScript has `streamSimple` and `completeSimple` for provider-neutral
reasoning levels, plus provider-specific options for lower-level control.
Go uses ordinary request options for both.

TypeScript:

```typescript
const response = await completeSimple(model, context, {
  reasoning: "medium",
});
```

Go:

```go
final, err := client.Complete(ctx, model, req, sigma.WithReasoningLevel(sigma.ThinkingLevelMedium))
```

Provider-specific controls use typed option structs where they are stable:

```go
final, err := client.Complete(
	ctx,
	model,
	req,
	sigma.WithOpenAIOptions(sigma.OpenAIOptions{
		ReasoningEffort:  sigma.ThinkingLevelMedium,
		ReasoningSummary: "detailed",
	}),
	sigma.WithAnthropicOptions(sigma.AnthropicOptions{
		ThinkingBudgetTokens: intPtr(8192),
	}),
	sigma.WithGoogleOptions(sigma.GoogleOptions{
		ThinkingBudgetTokens: intPtr(8192),
	}),
)
```

Thinking content appears as `sigma.ContentBlockThinking` in final messages and
as `EventKindThinkingStart`, `EventKindThinkingDelta`, and
`EventKindThinkingEnd` while streaming.

## Provider Options And Compatibility Flags

General request options are functions:

```go
final, err := client.Complete(
	ctx,
	model,
	req,
	sigma.WithAPIKey(apiKey),
	sigma.WithMaxTokens(1024),
	sigma.WithTemperature(0.2),
	sigma.WithHeader("X-Request-ID", requestID),
	sigma.WithSessionID("session-123"),
	sigma.WithCacheRetention(sigma.CacheRetentionShort),
)
```

Advanced provider-specific values can be carried through `ProviderOptions`:

```go
final, err := client.Complete(
	ctx,
	model,
	req,
	sigma.WithProviderOption(sigma.ProviderOpenRouter, "routing", map[string]any{
		"order": []string{"anthropic", "openai"},
	}),
)
```

For OpenAI-compatible custom endpoints, compatibility is model metadata:

```go
model := sigma.Model{
	ID:              "llama3.2",
	Provider:        sigma.ProviderCustom,
	API:             sigma.APIOpenAICompletions,
	Name:            "llama3.2",
	SupportedInputs: []sigma.ContentBlockType{sigma.ContentBlockText},
	SupportsTools:   true,
	OpenAICompletionsCompat: &sigma.OpenAICompletionsCompat{
		SupportsStore:                    sigma.OpenAICompatUnsupported,
		SupportsDeveloperRole:            sigma.OpenAICompatUnsupported,
		SupportsStreamingUsage:           sigma.OpenAICompatUnsupported,
		SupportsStrictTools:              sigma.OpenAICompatUnsupported,
		ReasoningFormat:                  sigma.OpenAICompletionsReasoningUnsupported,
		MaxTokensField:                   sigma.OpenAICompletionsMaxTokens,
		CacheControlFormat:               sigma.OpenAICompletionsCacheControlUnsupported,
		RequiresToolResultName:           sigma.OpenAICompatSupported,
		RequiresAssistantAfterToolResult: sigma.OpenAICompatUnsupported,
	},
}
```

Base URLs are provider adapter options:

```go
registry := sigma.NewRegistry()
if err := openai.Register(registry, sigma.ProviderCustom, openai.WithBaseURL("http://localhost:11434/v1")); err != nil {
	return err
}
if err := registry.RegisterModel(model); err != nil {
	return err
}
client := sigma.NewClient(sigma.WithRegistry(registry))
```

## Errors, Stop Reasons, And Cancellation

TypeScript reports stream failures through `error` events and final assistant
messages. Go does the same at the event/message level, but also returns typed
errors.

Key differences:

- `client.Complete` returns `(sigma.AssistantMessage, error)`.
- `sigma.Collect` returns the final assistant message even when a stream ends
  with an error event.
- Stream terminal errors may be `*sigma.GenerationError`, which carries the
  final assistant message.
- Use `errors.Is` with sentinel errors such as `sigma.ErrAborted`,
  `sigma.ErrNoProvider`, `sigma.ErrModelNotFound`, `sigma.ErrToolValidation`,
  `sigma.ErrProviderResponse`, and `sigma.ErrInvalidOptions`.
- Use `errors.As` for structured errors such as `*sigma.Error`,
  `*sigma.ProviderError`, `*sigma.GenerationError`, and
  `*sigma.ToolValidationError`.
- Stop reasons are Go constants, not the TypeScript strings. The closest common
  mappings are `stop` to `StopReasonEndTurn`, `length` to
  `StopReasonMaxTokens`, `toolUse` to `StopReasonToolCalls`, `error` to
  `StopReasonError`, and `aborted` to `StopReasonAborted`.

Cancellation uses `context.Context`:

```go
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

final, err := client.Complete(ctx, model, req)
if err != nil {
	if errors.Is(err, sigma.ErrAborted) || errors.Is(err, context.DeadlineExceeded) {
		fmt.Println("request was canceled")
	}

	var generationErr *sigma.GenerationError
	if errors.As(err, &generationErr) {
		if partial, ok := generationErr.FinalMessage(); ok {
			fmt.Println("partial stop reason:", partial.StopReason)
		}
	}
	return final, err
}
```

## Custom Models

TypeScript custom models often put `baseUrl`, `headers`, and `compat` directly
on the model object. In Go:

- `sigma.OpenAICompatibleModel` can carry custom base URL, headers, costs,
  context window, token limits, input support, reasoning support, and Chat
  Completions compatibility overrides.
- Provider adapters still own the HTTP implementation. Register
  `openai.Provider` on the same registry as the custom model metadata.
- Request/client options own credentials, retries, timeouts, and per-request
  overrides. Per-request headers override model headers, and resolved
  credentials override both.
- Plain `sigma.Model` values can still be built manually by setting
  `API: sigma.APIOpenAICompletions`.
- Register both the provider implementation and the model metadata on the same
  `Registry`.

See [examples/custom-model](../examples/custom-model/main.go) for a runnable
OpenAI-compatible local endpoint example.

## Context Serialization

TypeScript serializes `Context` directly. Go structs are also JSON-serializable,
but `AssistantMessage` and `Message` are distinct types. Persist the conversation
as messages, and convert assistant results before appending them.

```go
func assistantMessage(final sigma.AssistantMessage) sigma.Message {
	return sigma.Message{
		Role:       sigma.RoleAssistant,
		Content:    final.Content,
		Provider:   final.Provider,
		API:        "",
		Model:      final.Model,
		StopReason: final.StopReason,
	}
}

req := sigma.Request{
	SystemPrompt: "You are a helpful assistant.",
	Messages:     []sigma.Message{sigma.UserText("What is Go?")},
	Tools:        []sigma.Tool{weatherTool()},
}

final, err := client.Complete(ctx, model, req)
if err != nil {
	return err
}
req.Messages = append(req.Messages, assistantMessage(final))

data, err := json.MarshalIndent(req, "", "  ")
if err != nil {
	return err
}

var restored sigma.Request
if err := json.Unmarshal(data, &restored); err != nil {
	return err
}
restored.Messages = append(restored.Messages, sigma.UserText("Tell me more."))
```

For cross-provider replay, keep provider/model provenance on assistant messages
and check [provider-parity.md](provider-parity.md). Cross-provider context
handoff and capability-loss reporting are not yet complete Go parity.

## Auth, Environment, OAuth, And Browser Differences

The TypeScript package includes Node/browser-specific guidance, environment
checks, OAuth login helpers, and CLI credential storage. Go intentionally keeps
those concerns explicit:

- Use `sigma.WithAPIKey` for request-scoped static keys.
- Use `sigma.WithAuthResolver` or `sigma.WithProviderAuthResolver` to inject
  credential lookup.
- `sigma.EnvironmentAuthResolver` reads static API keys from environment
  variables based on model metadata and common provider names.
- OAuth token refresh and storage are application responsibilities. Provider
  adapters that need OAuth accept injected token providers, for example Codex
  Responses through `openai.WithCodexResponsesOAuthTokenProvider`.
- There is no browser build, no browser environment detection, and no frontend
  API-key warning path in the Go package.

## Intentionally Different

These TypeScript features are intentionally not carried over as direct Go API
parity:

- Browser-specific environment checks and browser runtime branches.
- TypeScript-only exports such as `Type`, `Static`, `TSchema`, and typed model
  literal unions.
- `streamSimple` and `completeSimple` as separate top-level helpers; Go uses
  `WithReasoningLevel` with `Stream`, `Complete`, or `CompleteText`.
- Automatic interactive OAuth login and credential persistence.
- Live provider/model discovery at request time; Go uses generated metadata and
  explicit registration.
- A full TypeScript-style cross-provider handoff layer. Provider-neutral message
  structures exist, but incomplete areas remain tracked in
  [provider-parity.md](provider-parity.md).
- Debug callbacks like `onPayload` and `onResponse` as public root options.
  Provider packages may expose deterministic tests and diagnostics, but the root
  package does not promise callback-name parity.

## Follow-up Notes

- Keep `provider-parity.md` updated when image generation, OAuth-backed
  providers, or cross-provider replay behavior changes.
- Revisit custom-provider examples as more OpenAI-compatible compatibility flags
  are promoted or renamed.
- Add dedicated examples for provider-specific OAuth token providers if those
  flows graduate from adapter-level configuration into public examples.
