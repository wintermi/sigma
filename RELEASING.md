# Releasing sigma

This is the repeatable, version-agnostic process for cutting a `sigma` tag. It
applies to every release. Per-version specifics — the change list and the
release story — live elsewhere:

- [CHANGELOG.md](CHANGELOG.md) is the canonical record of what changed in each
  version.
- `docs/release-notes-v<version>.md` (when present) carries the summary, scope
  classification, and serialization-compatibility boundary for that release.

Tag only from a reviewed, clean working tree after the validation below passes.

## Validation

The validation commands are deterministic and must not require live credentials
or network calls:

```sh
mise run go:test
mise run go:race
mise run go:vet
mise run go:generate
git diff --exit-code
```

Run the full CI-equivalent suite, which adds formatting and lint gates:

```sh
mise run ci
```

`mise run ci` runs `mise:validate`, `go:fmt:check`, `go:lint`, `go:vet`,
`go:test`, and `go:race`. It must be green, including `golangci-lint`, before
tagging.

Also review, as applicable:

- Package docs and exported API surface.
- The [provider parity matrix](docs/provider-parity.md) for new or changed
  provider rows.
- Generated model metadata drift (`mise run go:generate` produces no diff).
- Markdown internal links, checked through the Go test suite.
- Examples, built as part of `mise run go:test`.
- A repository secret scan. Confirm it surfaces only synthetic test fixtures and
  documented environment variable names.

## Release blockers

Before tagging any release, each blocker must be resolved or explicitly
documented as accepted risk:

- `mise run go:test` fails in a clean checkout.
- A public root-package API lacks docs, examples, or deterministic behavior
  coverage for its advertised use.
- Request, message, stream, tool, image, or provider-option JSON serialization is
  unstable or lacks round-trip coverage.
- Security tests do not cover credential precedence, redaction-safe errors, and
  provider failures for the providers being released.
- Configured static analysis or lint gates fail and maintainers have not
  accepted that backlog for the tag.
- A provider parity row claims MVP support without checked-in fixtures, golden
  payloads, fake clients, or cancellation/error coverage.
- README examples require live network calls for the default quick start.
- Default registry behavior implies a provider is runnable when only metadata is
  registered.
- Provider errors expose raw credential values or unredacted authentication
  payloads.

## Coverage and evidence standards

Every provider claim must point at checked-in fixtures, golden payloads, or fake
clients — never marketing assertions or generated metadata alone. Each feature
needs coverage in these categories where applicable:

- Stream contract: event ordering, terminal events, final assistant message,
  interleaved content indexes, and `Collect`.
- Fake provider: `sigmatest` request capture, scripted output, errors, and local
  image-provider behavior.
- Golden payloads: provider request JSON for branching behavior, tools, cache
  markers, image input, headers, and compatibility metadata.
- Cancellation: context cancellation before dispatch, during streaming, and
  before body exhaustion for supported providers.
- Errors and redaction: typed lookup errors, provider response errors,
  credential failures, retry boundaries, and redacted diagnostics.
- Docs examples: README and docs snippets must be deterministic or clearly mark
  credential requirements.

Preview providers should meet the same bar before being promoted to MVP in the
[provider parity matrix](docs/provider-parity.md).

## Non-goals

These are deliberately excluded from the Go package scope. Provider adapters and
the test suite should keep it this way unless a release note explicitly moves an
item in:

- Live provider calls in `mise run go:test`.
- Marketing claims that a provider is production-quality without deterministic
  fixtures or fake-client coverage.
- Automatic live provider/model discovery as a side effect of normal dispatch.
- Provider parity claims based only on generated metadata.
- Browser login, credential refresh, or durable persistence triggered
  implicitly by normal provider dispatch. Auth helpers and credential stores
  remain explicit and caller-owned.
- Hidden ambient credential loading inside provider adapters.

Deferred (not excluded) work is tracked in [TODO.md](TODO.md).

## Pre-tag checklist

- [ ] Confirm the release tag name and that it follows Major.Minor.Patch
      versioning.
- [ ] Confirm repository metadata on the hosting service points at
      `github.com/wintermi/sigma`.
- [ ] Confirm the MIT `LICENSE` file is present.
- [ ] Confirm `mise run ci` is green, including `golangci-lint`.
- [ ] Confirm `mise run go:generate` produces no metadata drift.
- [ ] Update [CHANGELOG.md](CHANGELOG.md): move `Unreleased` entries under the
      new version with its release date.
- [ ] Review [README.md](README.md) and the
      [provider parity matrix](docs/provider-parity.md) for accuracy.
- [ ] Confirm any breaking change before `v1.0.0` is documented in the changelog
      and release notes with upgrade guidance.
- [ ] Confirm the working tree is clean after validation.
- [ ] Tag only after maintainer acceptance; do not tag from an unreviewed tree.

## Git and commit conventions

Per [AGENTS.md](AGENTS.md), remote sync is handled by the maintainer; agents make
local commits only. Commit messages follow the Conventional Commits standard.
