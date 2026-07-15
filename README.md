# sigma

`sigma` is a Go package for provider-neutral AI model calls. The stable release
surface is text-first: one root API for model metadata, text streaming,
completions, tools, request persistence, custom OpenAI-compatible endpoints, and
deterministic tests. Other documented surfaces, including image generation and
some provider adapters, are preview or future work until release notes say
otherwise.

The module path is currently:

```sh
go get github.com/wintermi/sigma
```

The root package name is `sigma`. Version tags follow standard
Major.Minor.Patch numbering, starting with `v0.1.0`. Any breaking changes before
`v1.0.0` should be documented in [CHANGELOG.md](CHANGELOG.md),
[release notes](docs/release-notes-v0.6.0.md), and upgrade guidance. This
checkout is licensed under the [MIT License](LICENSE).

## Quick Start

The fastest path uses `sigmatest`, which is deterministic and makes no network
calls:

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/sigmatest"
)

func main() {
	provider := sigmatest.NewFauxProvider(sigmatest.Script{
		Final: sigma.AssistantMessage{
			Content: []sigma.ContentBlock{
				sigma.Text("Sigma provides provider-neutral model calls for Go."),
			},
		},
	})
	registry, err := sigmatest.Registry(provider)
	if err != nil {
		log.Fatal(err)
	}

	client := sigma.NewClient(sigma.WithRegistry(registry))
	text, err := client.CompleteText(
		context.Background(),
		sigmatest.TextModel(),
		"Write one short sentence about Sigma.",
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(text)
}
```

For a real provider, register a provider package on the same registry as the
model metadata and provide credentials through options, an auth resolver, or the
documented environment variables:

```go
package main

import (
	"context"
	"log"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/openai"
)

func main() {
	registry := sigma.NewRegistry()
	if err := openai.Register(registry, sigma.ProviderOpenAI); err != nil {
		log.Fatal(err)
	}

	model := sigma.Model{
		ID:              "gpt-4o-mini",
		Provider:        sigma.ProviderOpenAI,
		API:             sigma.APIOpenAICompletions,
		SupportedInputs: []sigma.ContentBlockType{sigma.ContentBlockText},
		SupportsTools:   true,
	}
	if err := registry.RegisterModel(model); err != nil {
		log.Fatal(err)
	}

	client := sigma.NewClient(sigma.WithRegistry(registry))
	text, err := client.CompleteText(context.Background(), model, "Reply in one sentence.")
	if err != nil {
		log.Fatal(err)
	}
	log.Println(text)
}
```

The OpenAI example above can use `OPENAI_API_KEY` through
`EnvironmentAuthResolver`, or `sigma.WithAPIKey("...")` for a request-scoped
override. Tests should prefer `sigmatest` or `httptest.Server`; the repository
test suite must not make live provider calls.

## Streaming

`Client.Stream` returns a single-consumer `*sigma.Stream`. Read ordered events
from `Events`, then inspect `Err` and `Final`, or call `sigma.Collect` when you
only need the final assistant message.

```go
stream := client.Stream(ctx, model, sigma.Request{
	Messages: []sigma.Message{sigma.UserText("Explain streaming briefly.")},
})
defer stream.Close()

for event := range stream.Events() {
	switch event.Kind {
	case sigma.EventKindTextDelta:
		fmt.Print(event.DeltaText)
	case sigma.EventKindToolCallDelta:
		// Tool-call JSON may arrive over multiple deltas.
	case sigma.EventKindDone, sigma.EventKindError:
		// Terminal events also carry the final assistant message.
	}
}
if err := stream.Err(); err != nil {
	log.Fatal(err)
}
final, ok := stream.Final()
_ = final
_ = ok
```

Text, thinking, and tool-call blocks can be interleaved. Use
`Event.ContentIndex` when building UI or transcript state.

## How Requests Flow

```text
Client
  -> Registry lookup for Model and Provider
  -> Provider.Stream or ImageProvider.Generate
  -> Stream events
  -> AssistantMessage
```

For text calls, `Complete` is implemented by collecting the provider stream. For
images, `GenerateImages` dispatches to a registered image provider and returns
`AssistantImages`.

## Documentation

- [Changelog](CHANGELOG.md) tracks release-visible changes and known
  limitations.
- [Release notes](docs/release-notes-v0.6.0.md) summarize the latest closed tag
  scope and compatibility boundary.
- [Releasing](RELEASING.md) documents the validation commands and pre-tag
  checklist used for every release.
- [TODO](TODO.md) lists deferred work that is outside the current release scope.
- [Providers](docs/providers.md) covers registration, credentials, environment
  variables, and caveats.
- [Streaming](docs/streaming.md) covers event handling and terminal messages.
- [Tools](docs/tools.md) covers schemas, validation, and tool-result replay.
- [Images](docs/images.md) covers image input and image generation.
- [Reasoning](docs/reasoning.md) covers thinking controls and streamed thinking
  blocks.
- [Errors](docs/errors.md) covers typed errors, cancellation, retries, and
  redaction-safe diagnostics.
- [Routing decisions](docs/routing.md) covers deterministic request
  classification, tiered model selection, and fallback advice.
- [Custom models](docs/custom-models.md) covers local OpenAI-compatible models
  and routers.
- [Testing](docs/testing.md) covers `sigmatest`, `httptest`, and live-test
  boundaries.
- [Request persistence](docs/persistence.md) covers JSON replay.
- [Inspired by `@earendil-works/pi-ai`](docs/inspired-by-pi-ai.md) maps
  familiar TypeScript concepts to Go.
- [Provider parity](docs/provider-parity.md) distinguishes implemented, partial,
  planned, unsupported, and preview provider features.
- [Security](docs/security.md) covers credential handling and diagnostic
  redaction.
- [Generated model metadata](tools/modeldata/README.md) describes catalog
  refreshes.
- [Examples](examples/README.md) lists runnable examples.

## Verification

```sh
mise run go:test
mise run go:race
mise run go:vet
mise run go:generate
git diff --exit-code
```

Run `mise run ci` for the full CI-equivalent suite (formatting, lint, vet, and
the race-enabled test run). The repository includes a Markdown internal-link
test and builds the examples as part of `mise run go:test`. External links and
live provider calls are not checked by default so verification stays
deterministic and does not require credentials.
