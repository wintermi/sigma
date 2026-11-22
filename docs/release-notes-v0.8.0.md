# Release notes: sigma v0.8.0

This is the maintainer-facing development note for the next `sigma` tag. Add
the v0.8.0 summary and scope as changes land. For the itemized change list see
[CHANGELOG.md](../CHANGELOG.md); for the validation commands and pre-tag
checklist see [RELEASING.md](../RELEASING.md).

## Release summary

Device-code login polling follows the server more closely. GitHub Copilot, Kimi Coding, and xAI used the server's poll interval as is, so a zero or very small value polled in a tight loop; every interval is now at least one second. xAI ignored the `interval` sent with `slow_down` and always added five seconds, so it could keep polling earlier than the server asked; it now uses the reported interval, as Copilot and Kimi do. Copilot also polled immediately after showing the code, before the user could approve it, and now waits one interval first like the other providers.

Custom credential stores have a documented cancellation contract. Sigma refreshes a stored OAuth token inside the `ModifyCredential` callback and detaches the refresh from the caller's cancellation, but passes the caller's context to the store. A third-party store that stopped writing when that context was canceled could drop a token the provider had already rotated, forcing a new login. `CredentialStore` and the cancellation guide now state that the context bounds only the lock wait, and the write must complete; `InMemoryCredentialStore` already behaves this way.

A failed save of refreshed OAuth credentials is retried. When an `OnRefresh` callback failed, the token providers kept the new credentials in memory, reported an error that discarded the callback's cause, and never tried to save again. Providers that rotate refresh tokens had already invalidated the stored one, so the next process start needed a fresh login. The error now wraps the cause (its message still omits it, since it may quote the credentials), and each later `Token` call retries the save before using the credentials; a failed retry does not block the request.

Bedrock no longer signs as the wrong identity when `AWS_PROFILE` names a profile without static keys. An explicit profile already took precedence over environment keys when it had static keys, but an SSO, `credential_process`, or `role_arn` profile, which sigma cannot resolve, fell through to whatever `AWS_ACCESS_KEY_ID` was set and silently used that account. With `AWS_PROFILE` set, environment keys are now skipped, as the AWS SDKs do, and the chain continues to the container, web identity, and instance sources, or reports credentials as unavailable.

GitHub Copilot device login enables models that need it. Copilot lists some models, such as Claude and Grok, with an "unconfigured" policy until the account enables them, and requests to them fail until then. Sigma's login returned credentials without enabling them, so these models failed after a successful login unless the caller called `EnableGitHubCopilotModels`. Login now enables the unconfigured models in sigma's Copilot catalog that the account offers. A failure there leaves login successful, and the models can still be enabled later.

Browser logins stop the paste-the-code prompt once the browser finishes. Anthropic, OpenAI Codex, and Radius pass `OnManualCode` a context so a caller can read a pasted code while the browser callback is pending, but the context outlived the login. A terminal prompt reading stdin therefore stayed active and consumed the next line the user typed. The context is now cancelled when login completes either way, as OpenRouter's login already did.

Radius browser login accepts any valid loopback redirect path. The callback server registered the caller's redirect path as an `http.ServeMux` pattern next to its own `/` fallback, so a redirect URI with path `/` registered the same pattern twice, and paths with spaces or braces failed pattern parsing. Both panicked the process after the callback port had been opened. The server now matches the path directly and answers other paths with a not-found page.

GitHub Copilot validates stored account base URLs, not just newly derived ones. The `proxy-ep` host check applied only when a base URL was derived from a fresh token, so a URL already saved in a credential store, from before the check or edited there, kept receiving the Copilot bearer token until the token was refreshed. Stored and supplied base URLs now go through the same trusted-host check and fall back to the token's own host. Enterprise domains configured with a port, such as `ghe.example.com:8443`, now match their hosts.

OAuth tokens are refreshed earlier. Sigma used a token until one minute before its recorded expiry, so a machine whose clock ran a minute or more fast, such as a VM or WSL host, or a request retried near the end of that minute, sent a token the provider already considered expired and got a 401. Stored credentials and every built-in OAuth token provider now refresh five minutes before expiry; `RefreshBefore` options still override it.

Anthropic browser login refuses to start on Windows when another process holds the callback port on IPv6 loopback. The earlier fix checked only for the Unix "address in use" error, which Windows reports as a different Winsock code, so login continued on IPv4 alone. A browser that resolves `localhost` to `::1` then sent the code and PKCE state to the other process. Any IPv6 bind failure now stops login unless it means the host has no IPv6 loopback.

Mistral no longer stores conversations by default. Sigma uses the Mistral Conversations API, which keeps each conversation on Mistral's side unless the request sets `store: false`, and sigma only sent it when the caller passed the `store` provider option. Every transcript was therefore retained by Mistral, although sigma sends the full history each turn and never continues a stored conversation. Requests now send `store: false` by default; set the `store` provider option to `true` to keep the previous behavior.

Cross-model handoff drops the previous model's thinking. `TransformRequestForModel`, the helper routing fallbacks use to adapt history, converted another model's thinking into `<thinking>`-tagged assistant text. The target then saw that reasoning as part of its own replies and could start writing reasoning into its visible answers, and because the thinking was already text, the provider adapters' own drop of foreign thinking no longer applied. Handoff now drops these blocks, keeps the text and tool calls, and reports each as a `thinking-dropped` change counted in `HandoffReport.DroppedThinkingBlocks`. `WithHandoffThinkingDelimiters`, `HandoffChangeThinkingConverted`, and `ConvertedThinkingBlocks` still compile but are deprecated and have no effect.

Switching to GPT-5.4 or GPT-5.5 after another model's tool calls works again. These models place tools loaded through `additional_tools` or tool search in a namespace named after the tool and reject replayed calls that lack it, and history from other models has no namespace, so the request failed with "Missing namespace for function_call". Their catalog rows no longer advertise dynamic tool loading, so deferred tools are declared up front; GPT-5.6 and later keep loading them dynamically.

GitHub Copilot reasoning and strict tools work as on the direct providers. Copilot reports reasoning tokens at the top level of `usage`, which sigma did not read, so thinking usage was always zero. Its Gemini 3.5 to 3.8 Flash and Kimi K3 rows were marked as not accepting a reasoning effort, so the requested thinking level was dropped; they now send `reasoning_effort` with the levels Copilot lists for each model, and Kimi K3 accepts `max`. Copilot Claude rows now advertise strict tool support, so tools that opt in are sent with `strict: true`.

Bedrock no longer drops caller-supplied Anthropic betas when reasoning is on. Thinking adds the interleaved-thinking or thinking-binding beta to `additionalModelRequestFields.anthropic_beta`, and it overwrote any betas the caller set there, so enabling reasoning silently removed betas such as the 1M-context beta and prompts over 200K tokens failed. The thinking beta is now added after the caller's betas, without duplicates.

DeepSeek, Together, Cerebras, Z.ai, and Cloudflare Workers AI streams report usage and cost again. Their compatibility defaults did not request streaming usage, and these endpoints only include a usage chunk when `stream_options.include_usage` is set, so the final message carried no usage and no cost estimate. Sigma now requests it for these hosts, as it does for other known providers and unknown endpoints; `OpenAICompletionsCompat.SupportsStreamingUsage` can still turn it off.

Whitespace-only tool results no longer fail requests. A tool that returned only whitespace, for example a shell command printing a bare newline, was sent to the provider unchanged, and some OpenAI-compatible providers reject such tool content with a 400. Chat Completions and Responses now send the same placeholder as for empty output, and Gemini sends an empty result as it does for empty output.

Bedrock reports Claude context-window stops as overflow. When the prompt and output fill Claude's context window, Bedrock ends the turn with `model_context_window_exceeded`. Sigma did not recognize it and returned an unclassified error, so callers could not tell they needed to compact. It now fails with a context-overflow provider error, the same treatment the direct Anthropic adapter gives this stop reason.

Gemini `CONTINUATION` finishes are max-token stops. Gemini reports `CONTINUATION` when generation reaches the per-request token limit before the answer is complete. Sigma treated the unrecognized reason as a failed turn and returned a provider error, so callers lost the partial answer and `IsRecoverableMaxTokens` could not see the stop. It now ends with `StopReasonMaxTokens`; sigma does not resume with the continuation token.

Codex WebSocket connections are retired after 45 minutes instead of 55. The backend closes connections at 60 minutes, and a turn that has started streaming cannot fall back to SSE, so a long high-effort response started on a connection close to the old 55-minute limit could be cut off mid-stream. The shorter window leaves 15 minutes for the response.

OpenAI Codex honors `MaxTokens`. Codex once rejected `max_output_tokens`, so sigma deleted it from every Codex request, and output caps were silently ignored: no `StopReasonMaxTokens`, and longer, costlier responses than requested. The backend accepts the field again, so Codex now sends it like the other Responses routes, at least 16 tokens, with the existing `SupportsMaxOutputTokens` opt-out for models that reject it.

OpenAI Codex keeps the system prompt in the prompt cache. Sigma sent the system prompt as top-level `instructions`, which the Codex backend places after the conversation from GPT-6.2 on, so each new turn changed the prompt ahead of it and the system prompt was re-read at full price. Codex requests now send it as the first `developer` input message, as the Codex client does, and send no instructions when there is no system prompt. Other Responses routes are unchanged.

Strict tool schemas with untyped nodes fail locally. OpenAI-compatible, Responses, Mistral, and Anthropic strict modes reject schema nodes without a type, but strict derivation accepted `{}`, `items: {}`, and bare `const` nodes and sent them, so the provider rejected the request with a 400. Derivation now reports "schemas without a type are unsupported" for the tool before dispatch; an `enum` still counts as typed.

Anthropic strict tools fail locally, with the tool and keyword named, when their schema uses a keyword Claude's strict mode rejects. A strict tool with `{"type":"integer","minimum":1}`, a `maxItems` bound, or an unsupported string `format` was sent as is, and Claude rejected the whole request with a 400. Sigma now checks every schema node against Claude's documented strict-mode limitations before dispatch, as it already did for references and unions; tools without strict metadata are unchanged.

OpenCode Claude Opus 5, Opus 5.5, and Fable 5.1 keep working after the system prompt or tools change. OpenCode forwards per-message effort and thinking block binding to Claude, but these rows did not enable them, so replayed signed thinking from an earlier prompt was rejected with "Invalid signature in thinking block" on every later turn. They now use mid-conversation effort like their direct Anthropic rows, which sends `drop_block` so stale thinking is dropped instead.

Claude models that reject temperature no longer receive it on any Anthropic Messages route. Opus 4.7 and later, Sonnet 5, and Haiku 5.5 reject the `temperature` field, but 13 catalog rows, including direct Claude Sonnet 5 and most Vertex Claude rows, did not record that, so a request with a temperature and no thinking failed. Those rows now mark temperature unsupported, and the adapter omits it.

`IsContextOverflow` and `ClassifyError` agree on throttled requests. A 429 with no structured code and wording like "Too many tokens, please wait" is rate limited by `ClassifyError`, but the diagnostic check behind `IsContextOverflow` lacked that rule and reported overflow, so callers compacted conversations that only needed a retry. Both now treat it as rate limiting.

OpenCode free-tier and Go subscription limits are no longer retried. OpenCode reports them as HTTP 429 with the error types `FreeUsageLimitError` and `GoUsageLimitError`, but sigma's code table held underscored spellings that the real codes never match, so the errors fell back to the 429 status and were retried as rate limits. Both codes are now classified as quota.

Z.ai plan usage caps are no longer retried. When a Zhipu or Z.ai plan reaches its usage limit, the API answers with HTTP 429 and error code `1308`, and the message is in Chinese, so sigma classified it as a rate limit and callers retried a request that cannot succeed until the cap resets hours later. Code `1308` is now classified as quota.

Numeric provider error codes are no longer dropped. Gateways that relay an upstream failure inside a stream often send `{"error":{"code":504,...}}` with no HTTP status. The numeric code was decoded as a float and ignored, so the error had neither a code nor a status and was not retried. Numeric codes are now kept when the error has no textual code, type, or status, and an HTTP status sent this way classifies the error like a response status, after any overflow or quota wording in the message.

More mid-stream gateway failures are retryable. Errors relayed inside a stream carry no HTTP status, so their class depends on the code and message alone, and wording such as "Upstream request timed out", "read ECONNRESET", "stream disconnected before completion", "The provider ended the response before it was complete.", or the Codex WebSocket connection limit was classified as unknown and not retried. These, the other common gateway phrasings, and standalone `5xx`, Cloudflare `52x`, and `429` statuses in the message text now classify as transient or rate limited. Status numbers are only consulted when nothing else matches, and never inside a longer number or word, so overflow and quota messages keep their class.

Responses reasoning models keep their reasoning between turns when no reasoning level is set. Leaving the level unset still uses the provider's default effort, but sigma only requested `reasoning.encrypted_content` when a level was set. With `store: false`, the default-effort reasoning came back without encrypted content and was dropped from the next request, so the model lost its reasoning across tool calls while the reasoning tokens were still billed. Reasoning models now request the encrypted content whenever reasoning may run; explicit `ThinkingLevelOff` requests are unchanged.

Bedrock Claude structured output works with a reasoning level set. The response format forces Claude to call a synthetic tool, and Claude rejects thinking together with a forced tool choice, so these requests always failed with a 400. Sigma now leaves thinking off when the response format tool is forced; other Bedrock models and requests without a response format are unchanged.

Deferred OpenAI Responses calls now cap the response body at 64 MiB, the limit image responses already use. Submit, fetch, and cancel decoded the body with no limit, the only unbounded provider read left, so an endpoint or proxy returning an endless body kept the client reading; an oversized body now returns a provider response error.

Radius gateway catalogs can no longer send model requests to another host. The catalog's `baseUrl` decides where model requests go, carrying the Radius credential, and any http(s) URL was accepted, including plain HTTP or an unrelated host. A catalog whose base URL is not on the gateway's scheme, host, and port is now rejected and leaves the registered models unchanged; a different path on the gateway origin still works.

GitHub Copilot no longer sends its token to an arbitrary `proxy-ep` host. The token names the account's API host, and sigma used whatever it contained as the request host for every bearer-authenticated call. Only hosts under `githubcopilot.com`, `ghe.com`, or the configured enterprise domain are now used; other values, including ones with ports, paths, or userinfo, fall back to the enterprise or individual endpoint.

HTTP redirects no longer carry provider credentials to another host. Go removes only `Authorization` and cookies when a redirect changes host, so API key headers such as `x-api-key`, `x-goog-api-key`, Azure `api-key`, and `cf-aig-authorization` reached whatever host a provider, proxy, or compromised endpoint redirected to, even over plain HTTP. Provider, OAuth, and AWS credential requests now keep only `Accept`, `Accept-Encoding`, `Accept-Language`, and `User-Agent` on cross-origin redirects, and refuse redirects that downgrade from HTTPS or would resend the request body to another origin. Same-origin redirects behave as before, and a caller's `CheckRedirect` still runs without its client being modified.

The OpenAI Codex catalog drops `gpt-5.5` and `gpt-5.3-codex-spark`. The Codex backend rejects both for ChatGPT accounts, so a model chosen from the registry failed on every request. The surface probe now defaults its Codex checks to `gpt-5.6-terra`, and direct OpenAI and other routes still list these models.

Thinking models without level metadata no longer claim `xhigh` and `max`. `Model.SupportsThinkingLevel` already required an explicit map entry for these levels when a model had a `ThinkingLevelMap`, but returned true for any level on thinking models with no map, such as GPT-5, o3, and Claude Sonnet 4.5, so callers and adapters accepted levels those models do not offer. The rule now applies to every thinking model. Claude Sonnet 4.6 rows map `max` and Sonnet 5 rows map `xhigh` and `max` on each route, and Cloudflare GLM-5.2 and OpenCode Qwen3.8 Flash map the levels they accept, so supported levels are unchanged.

The Anthropic browser login can no longer be intercepted through the IPv6 loopback address. The callback server listened only on `127.0.0.1`, but the redirect URI uses `localhost`, which browsers may resolve to `::1` first. Another local process listening on `[::1]` at the same port could therefore receive the authorization code, and since Anthropic's flow uses the PKCE verifier as the state value, it would hold everything needed to redeem the token. The login now listens on both loopback addresses and moves to another port when the IPv6 side is already taken.

Bedrock credential lookups from the ECS container endpoint and EC2 instance metadata service no longer go through `HTTP_PROXY`. Go skips environment proxies only for loopback addresses, not the link-local metadata addresses, so on hosts with a proxy configured and no matching `NO_PROXY`, the metadata session token, the EKS Pod Identity token, and the returned AWS keys passed through the proxy, and a forward proxy could even reply with credentials for its own role. These requests now connect directly unless the caller supplies an HTTP client; STS web-identity requests still honour the proxy.

Direct Anthropic models with budget thinking, such as Claude Sonnet 4.5, now get the same default budgets as Bedrock Claude: 8,192 tokens at `medium` and 16,384 at `high` and `xhigh`. The direct adapter used half those budgets for `medium` and `high`, so the same reasoning level produced shallower thinking than on Bedrock and less than sigma's own token estimates assumed. Budgets still clamp to leave 1,024 output tokens within `max_tokens`, and explicit budgets are unchanged.

Responses history replay no longer sends an assistant message's original `msg_` ID when the reasoning item it belongs to is dropped, for example after switching between models or when the reasoning has no encrypted content. OpenAI ties such message IDs to their reasoning item and can reject the request when the reasoning is missing. These messages now get a generated ID, as tool call items already did, while same-model turns whose reasoning is replayed keep their IDs.

Responses session affinity now uses the `session-id` header that OpenAI documents, as Codex requests already did. sigma sent `session_id`, which nginx-style proxies silently drop because the name contains an underscore, so cached sessions lost routing affinity and their prompt-cache hit rate fell. `x-client-request-id` and the custom session header option are unchanged.

Anthropic's internal `api_error`, which arrives as a stream error event once a response has started and so carries no HTTP status, is now classified as transient and retryable instead of unknown. Routing and retry logic built on `ClassifyError` previously gave up on it, although Anthropic's own message asks the client to try again. Gateway failures worded as "connection closed", "stream was interrupted", "service is temporarily unavailable", or "returned an empty response", among others, are now transient too, and underscored `rate_limit` wording counts as rate limiting.

Opt-in HTTP retries now survive ordinary rate limits. The default maximum retry delay was 2 seconds, so a 429 asking the client to wait 5 seconds, which OpenAI and Anthropic commonly send, failed at once with `ErrRetryAfterExceedsMaxDelay`, and responses without `Retry-After` were retried after 100ms and 200ms. The default cap is now 60 seconds, and backoff starts at 500ms and doubles up to 8 seconds with up to 25% jitter, matching the OpenAI and Anthropic SDK retry policies. `WithMaxRetryDelay` still overrides the cap, and retries remain off unless `WithMaxRetries` is set.

OpenRouter app attribution configured on the provider is sent again. OpenRouter catalog models include empty `HTTP-Referer` and `X-Title` header placeholders, and sigma applied model headers after provider headers, so `openrouter.WithHeader("HTTP-Referer", ...)` was overwritten with an empty value on every catalog model. Empty model header values are now skipped for text and image requests; non-empty model headers keep their precedence.

OpenRouter costs for bring-your-own-key requests are no longer understated. For these requests OpenRouter's `usage.cost` is only its own fee, often $0, while the inference itself is billed to the caller's provider account and reported in `cost_details`. sigma reported the fee as the provider cost and dropped `is_byok` and `cost_details` from `Usage.Raw`. The provider-reported cost now adds the upstream inference cost, is omitted when OpenRouter does not supply it, and the raw accounting fields are kept.

`ValidateToolCall` now accepts two common model habits. A `null` in an optional property whose schema does not allow null, such as `{"limit": null}` for an optional integer, is treated as leaving the property out for every tool; sigma only did this for strict tools, so these calls failed with "expected integer, got null". With `CoercePrimitives`, a single value sent where an array is expected, such as `{"paths": "a.txt"}`, becomes a one-item array. Nullable properties keep explicit nulls, and required properties still reject null.

Bedrock requests whose model ID is a system inference profile, provisioned model, imported model, or foundation model ARN now go to the region in the ARN. sigma only read the region from application inference profile ARNs, so other ARNs were signed for and sent to `AWS_REGION`, which fails for an ARN in another region, or failed with "region is required" when no region was set. ARNs passed through the `model_id` provider option are also recognised. An explicit `WithRegion` or region provider option still wins.

Switching a conversation that contains thinking to a Mistral model without thinking support, such as Codestral or Devstral, no longer fails before sending. A capability check ran on the history before the replay transform and rejected every thinking block, even though the transform drops thinking from other models and converts the target model's own thinking to text.

Gemini tool calls now respect constant parameters and discriminated-union tags. Gemini ignores `const` in `parametersJsonSchema` but honours `enum`, and sigma sent `const` unchanged, so the model could emit any value and `ValidateToolCall` then rejected the call or matched the wrong branch. String and number `const` values are now sent as one-value `enum`s, recursing only through subschemas so literal data such as `default` is left alone.

Anthropic requests no longer fail because one tool schema has a root `anyOf`, `oneOf`, or `allOf`, or no root `type`, as some MCP and generated schemas do. sigma forwarded tool schemas unchanged and Anthropic rejected the whole request with a 400. Non-strict tool schemas are now rebuilt as a root object with their properties, required list, additional-properties setting, and shared definitions.

Claude Opus 5, Opus 5.5, and Fable 5.1 requests now work for organizations that cap effort below `high`. These models take their effort from per-message markers, and sigma also sent a fixed top-level `output_config.effort` of `high`, which capped organizations reject at any requested level. The fixed top-level value is now `low`; the markers still set the effort for each reply, and the cached prompt prefix is unchanged between requests.

Turning reasoning off on Mistral Small 2603 and Medium 3.5 now reaches Mistral as `reasoning_effort: "none"`. sigma sent no effort for off, so these adjustable-reasoning models kept their default effort. Requests that leave the level unset still use the provider default.

Claude Opus 5 can now run with thinking turned off. sigma rejected `ThinkingLevelOff` on every model with mid-conversation effort, but Opus 5 accepts disabled thinking. Off now sends `thinking: {type: "disabled"}` without effort markers, since effort cannot change while thinking is disabled. Opus 5.5 and Fable 5.1, which reject disabled thinking, keep off marked unsupported and still fail locally.

Turning reasoning off on OpenAI Responses, Azure, and Chat Completions reasoning models now reaches the provider. Off previously sent no reasoning field, which these models treat as "use the default effort", so they kept reasoning and billing reasoning tokens. Responses and Azure now send the model's mapped off effort or `none`, as Codex already did, and Chat Completions `reasoning_effort` routes send the mapped off value when the model has one. GitHub Copilot, which rejects an off effort, and requests without a level are unchanged.

Thinking recorded by a different provider, API, or model is no longer replayed to the next model. Anthropic, Bedrock, and Mistral received it as assistant text inside `<thinking>` tags and Gemini as bare text, so the new model saw another model's reasoning as part of its own replies and could start writing its reasoning into the visible answer; Chat Completions routes that require `reasoning_content`, such as DeepSeek, received it as the model's own chain of thought. These blocks are now dropped, while text and tool calls are kept. Messages without provenance are still treated as the target's own.

Responses function tools without strict metadata now send `strict: false`. The Responses API, and GitHub Copilot in particular, treats an omitted `strict` as `true`, so these tools ran in strict mode on their original schema: every optional property became a required string, and models filled them with empty strings or invented values. Tools marked strict are unchanged.

Bedrock requests to adaptive-thinking Claude models no longer switch to budget thinking when the caller sets `ThinkingBudgetTokens`. These models only accept adaptive thinking, so a request with an explicit budget sent `budget_tokens`, which they reject, and lost thinking block binding. The budget is now ignored for these models and the reasoning level selects the effort, as on the direct Anthropic adapter. A zero or negative budget still disables thinking.

Gemini and Vertex text turns that end with `OTHER`, `FINISH_REASON_UNSPECIFIED`, or a finish reason sigma does not recognise now fail. Google uses these reasons when generation stops for reasons other than completion, token limits, or safety, but sigma finished them as successful turns with an `unknown` stop reason, so cut-off answers looked complete and agent loops neither retried nor reported them. They now end with `StopReasonError` and a `ProviderError` naming the reason, like rejected tool calls, while partial output, usage, and the raw finish reason are kept. Image generation responses are unchanged.

OpenAI Chat Completions turns that end with an unrecognised `finish_reason` now fail. Providers use such reasons for interrupted generations, for example DeepSeek's `insufficient_system_resource`, but sigma finished them as successful turns with an `unknown` stop reason, so truncated answers looked complete. They now return a typed provider error naming the reason, while the final message keeps the partial output, usage, and raw `finish_reason`; the recognised reasons, including `content_filter`, are unchanged.

Codex Responses no longer reports failed responses as successful turns. Codex can end a stream with `response.done` whatever the outcome, and sigma treated every `response.done` as completion, so a `failed` or `cancelled` response finished as an ordinary end turn with whatever content had streamed, an `incomplete` response finished with an unknown stop reason, and on the WebSocket transport the failed response was cached as the next turn's `previous_response_id`. The response status now decides: failed and cancelled responses return errors, incomplete responses follow `response.incomplete` handling, and only completed responses finish successfully.

Bedrock one-hour prompt caching now uses the Converse wire value. With `CacheRetentionLong` or `CacheRetentionPersistent`, Claude cache points carried `ttl: "ONE_HOUR"`, the AWS SDK enum name, rather than its value `"1h"`, and usage parsing looked for the same name, so one-hour cache writes reported by Bedrock were counted as five-minute writes and priced at the five-minute rate. Requests now send `"1h"`, and `LongCacheWriteInputTokens` counts the `cacheDetails` entries Bedrock reports with that TTL.

GitHub Copilot business, enterprise, and data-residency accounts now reach their own endpoint. The Copilot token names the account's API host in its `proxy-ep` field, and sigma computed that URL and kept it in the credential metadata, but nothing used it, so every request went to `api.individual.githubcopilot.com` with a token issued for another host. Stored Copilot credentials now supply the host through the new `OAuthAuth.BaseURL` hook, and `GitHubCopilotOAuthTokenProvider` implements `AuthResolutionResolver`; Anthropic Messages models receive the host with its `/v1` prefix. Individual accounts keep their existing endpoint, and an explicit request `base_url` provider option still takes precedence.

A failed stored OAuth credential no longer silently switches the request to an API key. The client's auth chain moved on to environment keys whenever the stored-credential resolver returned an error wrapping `ErrCredentialUnavailable`, which refresh failures and the documented `WithOAuthMinimumValidity` shortfall both do, so with `ANTHROPIC_API_KEY` also set a subscription login that could not refresh was billed to the metered key without notice. A stored OAuth credential now owns its provider: those failures end the request, and environment keys apply only when nothing is stored or the credential was removed during the refresh.

Xiaomi MiMo reasoning requests no longer fail. MiMo Chat Completions documents only `thinking.type` enabled or disabled, but the catalog rows left `reasoning_effort` enabled and exposed every level, so a request at the `minimal` level, among others, sent `reasoning_effort` and was rejected with a 400. The nine API-billing and token-plan rows now send only the toggle and accept `off` and `high`; Xiaomi documents every effort level as equivalent. Other levels return a local invalid-options error.

Anthropic and Bedrock Claude conversations no longer fail after a tool returns an error with an image. Both APIs reject error tool results that contain anything but text, so once a failed screenshot or render tool result was in the history every later request was rejected. The images of error tool results now follow all of the turn's tool results in the same user message, introduced by a caption naming the tool call, and the result itself keeps a text placeholder. Successful tool results, and error results for non-Claude Bedrock models, keep their images in place.

OpenAI Responses requests no longer send a thinking budget. The provider-neutral `ThinkingBudgetTokens`, which `WithReasoningBudgetForContext` sets, was copied into `reasoning.budget_tokens`, a field the Responses API does not define, so OpenAI, Azure, and Codex reasoning requests built with the budget helper sent an unknown parameter; a budget alone also turned on `reasoning` and the encrypted-reasoning include for models without a level. Responses now uses effort only.

Tool-call coercion of recursive unions no longer takes exponential time. With `CoercePrimitives`, arguments that fail validation are coerced branch by branch, and every union branch was coerced into, including its nested values, before its discriminator was checked; with a recursive filter schema each nesting level doubled the work, so model-supplied arguments nested 18 levels deep took about 16 seconds whether or not coercion could fix them. Branches whose own or property type, enum, or const cannot match even after coercion are now skipped first.

`IsContextOverflow` now detects overflow reported mid-stream. It reads the diagnostics on the final message, but the OpenAI, Anthropic, Google, Vertex, Azure, and Codex adapters attached diagnostics only for HTTP status errors, so a `response.failed` with `context_length_exceeded` or an Anthropic `model_context_window_exceeded` stop classified as overflow through `ClassifyError` while the stored message did not. The stream writer now attaches the provider diagnostic when an error final message has none.

Partial-message accumulation is linear for providers that send only `DeltaText`, such as Radius and custom `NewStream` providers. Text, thinking, and tool-argument deltas were appended with string concatenation, copying the whole block on every event, so 2 MiB streamed in 16-byte deltas took about 18 seconds; blocks now use a builder.

Long streamed tool arguments no longer slow streaming quadratically. Every delta re-parsed, repaired, and completed the whole accumulated argument text to refresh `PartialToolCall.ProviderMetadata["arguments"]`, so a tool call writing a 256 KiB file in 32-byte deltas spent about 13 seconds parsing. Partial arguments are still decoded on every delta up to 16 KiB; beyond that the decoded value is refreshed each time the text grows by an eighth. `argumentsText`, `ArgumentsDelta`, and final arguments are unchanged.

Tool-call validation of recursive unions no longer takes exponential time. Object properties were validated in name order, so a union branch with the wrong discriminator, such as `op: "and"` when the value is `"or"`, still validated its nested `args` before failing on `op`; with a recursive filter schema this doubled the time per nesting level, and 454 bytes of model-supplied arguments at depth 20 took several seconds. Each property's own type, enum, and const are now checked before any nested value is validated.

Bedrock Claude responses are no longer truncated at 4,096 tokens. The adapter only sent `inferenceConfig.maxTokens` for an explicit `MaxTokens` or manual thinking, and Bedrock applies a 4,096-token default otherwise, cutting off long answers and adaptive thinking. Claude requests now default to the model's output limit; other Bedrock model families keep the provider default.

Direct OpenAI Chat Completions requests now send `max_completion_tokens`. The OpenAI compatibility profile selected `max_tokens`, which OpenAI rejects for o-series and GPT-5 reasoning models, so any request with a max-token limit on those models failed. Compatible providers that require `max_tokens` keep it.

Gemini models before Gemini 3 now receive parallel tool results in one turn. These models take tool-result images as a separate "Tool result image:" user turn, which was inserted directly after its function response, so later responses landed in another turn and Gemini rejected the request because function response parts no longer matched the function calls. Image turns are now emitted once the run of tool results ends.

Splitting large documents for retrieval no longer slows quadratically: each chunk looked up its separator position by scanning every rune from the start of the text, and now uses a binary search.

The retrieval splitter handles invalid UTF-8. It measured each rune with `utf8.RuneLen`, which is 3 for the replacement rune although an invalid byte occupies 1, so text ending in a bad byte panicked with a slice-bounds error and a bad byte mid-text produced overlapping, duplicated chunks. Spans now follow the bytes actually decoded.

OpenCode keeps same-model replay state across turns. The wrapper rewrites `Model.API` to the routed wire API, but callers persist the catalog `Model.API`, so every recorded turn failed the adapters' exact provenance check and lost its encrypted reasoning, signatures, and tool item IDs. History from the same OpenCode model is now replayed under the routed API.

OpenAI Responses replay no longer sends a tool call's item ID after dropping the reasoning item it was paired with. A reasoning item is only replayable with encrypted content (or stored items), so reasoning from a request without `reasoning.encrypted_content`, or from another OpenAI-family provider such as Codex, was dropped while its `fc_` ID was kept, and OpenAI rejected the turn. Tool calls after a dropped reasoning item now omit `id`, as calls from a different model already did; calls after a replayed reasoning item keep it.

Throttling is no longer mistaken for context overflow. Bedrock puts the exception type in the `X-Amzn-Errortype` header, which the adapter now records as the provider code, so a `429` reading "Too many tokens, please wait before trying again." is rate limited. Adapters that flag overflow from loose body text keep `ErrContextOverflow` only when the classifier agrees, so a `rate_limit_exceeded` code or an upstream "context deadline exceeded" stays retryable, and a `429` without a structured code is rate limited.

Tool schemas keep their empty objects on the wire. The shared request transform copied an empty `map[string]any` as a nil map, so a no-argument tool built from decoded JSON was sent with `"properties":null` (and `"items":{}` or an any-typed `{}` became `null`), which providers reject. Typed containers such as `map[string]sigma.Schema` were also shared with the caller; they are now deep-copied.

`sigma-evals-runner` accepts an explicit `-artifact-dir` or `SIGMA_EVAL_ARTIFACT_DIR` outside a Go module. The module root is only used to shorten recorded caller paths, so those paths stay absolute when no module is found. The default `.eval/` location still requires running inside the module.

Surface-probe handoff mode bounds each source context and each target replay with `-case-timeout`, as surface and image cases already were. Previously, a stalled handoff request could run until the overall `-timeout`.

The surface-probe README no longer says `no_working_attempt` covers failed text repairs. The outcome comes from image probes that complete without the required image or tool output, and from failed handoff model discovery. A text case whose repair variants all fail keeps its original classification.

The surface probe's image cases retry transient HTTP 5xx and connection-reset failures twice with the identical request, as text cases already did and the README describes. Previously, a single image-route 5xx was reported straight away as upstream availability.

`DotProduct`, `CosineSimilarity`, `NormalizeEmbeddingVector`, `CombineEmbeddingVectors`, and `RankEmbeddingsByCosine` reject vectors containing NaN or infinity with the new `ErrEmbeddingVectorNonFinite` sentinel. Previously, such values passed through as NaN scores, which also left rankings in an arbitrary order.

`Client.Embed` checks that a provider returns one vector per input on every call, not only inside the batcher. A short or long response now returns an `ErrorProviderResponse` error with no vectors, so callers cannot pair vectors with the wrong inputs.

The `sigmatest` faux image and embedding providers fail when called with no queued script, matching the faux text provider. Tests that make more calls than they scripted now fail loudly instead of passing on an empty result.

Gemini image requests turn the portrait and landscape sizes `1024x1536` and `1536x1024` into the `2:3` and `3:2` aspect ratios. Previously, they were dropped and the model default was used. Imagen has no such ratios, so those sizes still leave its aspect ratio unset.

OpenAI image edits that upload more than one image name the file parts `image[]`, matching the documented multi-image form and the OpenAI SDKs. Single-image edits and variations still send `image`.

OpenAI and Google image generation now handle over-limit success bodies the same way OpenRouter does. A response over 64 MiB returns a `ProviderError` with the HTTP status and a size-limit diagnostic, instead of a silently truncated body that failed JSON decoding.

Radius requests use the gateway's current transcript context shape. The system prompt and tool declarations are carried by a leading `{"role":"system","content":...,"toolsAdded":[...]}` message instead of the legacy `context.systemPrompt` and `context.tools` fields. Requests with neither a prompt nor tools send only the conversation messages.

Radius forwards the provider-neutral `ToolChoice` option as `options.toolChoice`. Previously, `none` and other tool choices were silently ignored on this route.

Bedrock event-stream frames with `:message-type: error` now end the stream with a classified provider error carrying the frame's `:error-code` and `:error-message`. Previously they were skipped, and the stream could end without a terminal error.

Bedrock tool specs omit `description` for tools without one. Converse requires a non-empty description when the field is present, so tools with blank descriptions were rejected.

Vertex Anthropic requests send the same `anthropic-beta` header as direct Anthropic requests. Previously, Vertex silently dropped configured betas such as `context-1m-2025-08-07` and the automatic tool-streaming and thinking betas.

Kimi Coding token refresh retries transient failures (transport errors, rate limits, and server errors) up to three times with exponential backoff. Other rejections, such as `invalid_grant` or 401, still fail immediately so the caller can prompt for a new login.

GitHub Copilot device login follows the poll interval GitHub sends with a `slow_down` response. The fixed five-second increment remains the fallback when the server omits an interval. This avoids polling early repeatedly and timing out.

Bedrock applies the AWS SDK host rule to `AWS_CONTAINER_CREDENTIALS_FULL_URI`. HTTPS endpoints may use any host. Plain HTTP endpoints must be loopback, `169.254.170.2`, `169.254.170.23`, or `fd00:ec2::23`; any other value is reported as a container credential error instead of being fetched.

Credential structs no longer print secrets when formatted with `%v`, `%+v`, `%#v`, or `%s`. `StoredCredential`, the Anthropic, Codex, Copilot, Kimi, OpenRouter, Radius, and xAI OAuth credentials, Bedrock `StaticCredentials`, and `openai.AzureAccessToken` now follow `sigma.Credential` and print a description that names the secret fields without their values. JSON encoding is unchanged.

Bedrock's default chain honours an explicit `AWS_PROFILE` ahead of static environment keys, as the AWS SDK for JavaScript does. Previously, ambient access keys silently overrode the selected profile. Without `AWS_PROFILE`, or when the profile has no static keys, the environment keys are still used.

The environment resolver recognizes `GEMINI_API_KEY` for `google` models, the variable Google AI Studio documents. `GOOGLE_API_KEY` and `GOOGLE_CLOUD_API_KEY` still take precedence when set.

Tool-call validation and coercion walk object properties in sorted name order. When more than one property fails, the reported error no longer varies between runs with Go map iteration order.

Tool-argument coercion from a JSON number to a string keeps the exact decimal value. Large integers and long decimals no longer lose precision by passing through `float64`.

OpenRouter requests for non-Anthropic models no longer carry message-level `cache_control`. Long retention previously sent `{"type":"persistent"}`, which OpenRouter does not define. Anthropic routes keep their explicit breakpoints, and an explicit `CacheControlFormat` compat override still opts in.

Chat Completions compat detection matches xAI and Z.ai base URLs by domain or subdomain instead of by substring. Custom endpoints on hosts that merely end in `x.ai` or `z.ai` now get the default compat settings.

Chat Completions history skips assistant turns that contain only reasoning. Previously, such a turn (for example, an aborted DeepSeek response) was replayed with `reasoning_content` and no `content`, which compatible servers reject.

Chat Completions tool results join multiple text blocks with newlines instead of concatenating them. Empty results send `(no tool output)`, since some compatible servers reject empty tool content.

Chat Completions tool-call streaming now matches deltas by stream index and then by provider id. Separate index-less calls stay separate, and a call whose continuation carries only its id stays whole.

Vertex express mode is supported for native Gemini text. When an API key is supplied without a project ID, requests go to the global `publishers/google/models/{model}` route instead of failing locally. OAuth credentials still require a project and location.

Thinking carried over from another model is now replayed to Gemini as plain text
rather than inside `<thinking>` tags, so Gemini no longer copies the tags into
its responses. Other adapters keep their existing delimiters.

Google and Vertex responses that finish with `TOO_MANY_TOOL_CALLS` now end with
a typed error, matching the other tool-call failure reasons, rather than a
successful completion whose partial tool calls a caller might execute.

Cerebras context overflows, which arrive as an HTTP 400 with no body, now
classify as context overflow with split-recoverable advice. HTTP 413 keeps its
existing overflow classification, which embedding batch splitting relies on.

HTTP retries now follow a provider's `x-should-retry` header, retrying a
response the provider marks retryable and stopping on one it marks final, and
accept fractional `Retry-After` and `Retry-After-Ms` delays. The retry
documentation now lists 408 and 409 and describes a zero `WithMaxRetryDelay`.

Cancelled streams now always deliver their synthesized terminal event. When a
slow consumer had left an earlier event unread, the terminal event was dropped
and the channel closed without one; the stale event is now replaced instead.

When sigma's own `WithTimeout` elapses during a text stream, the stream now ends
with `StopReasonError` and a transient error that still matches
`context.DeadlineExceeded`, so retry and route-fallback advice apply. Caller
cancellation and caller-owned deadlines remain aborts.

Cached Codex WebSocket connections are now retired after 55 minutes, before the
backend's 60-minute connection limit, so long sessions open a fresh connection
instead of failing on a connection the server is about to close.

Codex requests with `CacheRetentionNone` now drop session affinity entirely,
omitting session headers and the prompt cache key and skipping cached WebSocket
continuation, matching the direct Responses adapter and the reference
implementation.

Codex requests with `WithReasoningLevel(ThinkingLevelOff)` now send the model's
off effort (`none` for GPT-6 Luna and Sol) rather than omitting reasoning, which
ran the request at Codex's default effort.

Anthropic subscription requests now identify as Claude Code 2.1.280 rather than
the outdated 2.1.75.

Token estimates now use 3.5 characters per token instead of 4, matching the
reference implementation. Context-based output limits are slightly more
conservative, reducing context-limit request failures.

Generated Azure OpenAI Responses models now send `api-version=v1`, the default
of the `/openai/v1` API they call. The previous `2025-04-01-preview` value is a
legacy-route version outside the v1 API's accepted values (`v1`, `preview`).
`azure.WithAPIVersion` still overrides the value per request.

`sigma-surface-probe` now redacts the request credential and recognized secret
shapes from provider error bodies it reports during model discovery and
Fireworks capability lookups, matching the redaction applied elsewhere.

`EmbedBatch` splits and averages an input larger than the batch byte limit only
when `SplitOversized` is set. Without it, the call now fails with
`ErrInvalidOptions` before any provider request instead of returning a lossy
averaged vector the caller did not ask for.

Bedrock Nova, Titan image, and variable-size Cohere embeddings now honor
`EmbeddingRequest.Dimensions`, and Nova query embeddings use the
`GENERIC_RETRIEVAL` purpose. Explicit provider options still take precedence,
and fixed-size Cohere v3 requests are unchanged.

`SplitRetrievalText` no longer crawls forward one rune at a time after a chunk
shorter than the overlap, which produced suffix fragments such as `" Heading"`
and `"eading"`, and it omits whitespace-only chunks that embedding rejects.
`AddDocuments` therefore succeeds for documents that open with a short heading.

Radius final messages now carry a cost estimate from the gateway catalog's
pricing, as other adapters do. Radius also keeps text and tool-call thought
signatures from the stream and replays them, so tool loops on Gemini-backed
gateway models no longer lose the signatures those models require.

Responses that report `service_tier: "fast"`, as GPT-6 Fast mode does, are now
priced like priority processing instead of at the standard rate.

Fourteen Mistral models and the two Bedrock Ministral 3 models now advertise
image input, so image requests reach the provider instead of failing local
capability checks.

Catalog pricing corrections: direct MiniMax-M3 now charges its base rate below
512K input tokens and accepts its full 1M context and 512K output; 27 Mistral
rows price cached input at a tenth of the input rate rather than zero; and 28
geographic and in-region Bedrock Claude profiles include AWS's 10% regional
premium. All values match the reference catalog.

The models.dev catalog refresh now imports context-length price tiers, filling
rates a tier omits from the base price, so refreshed and newly added rows carry
current long-context pricing. Rows whose source lists no tiers keep their
curated tiers.

Twenty text rows gain the long-context price tiers their providers charge,
matching the reference catalog: direct Gemini 2.5 Pro and 3.1 Pro, xAI Grok 4.3
and 4.5, OpenCode and OpenCode Go Grok, GPT, Gemini, MiniMax M3, and Qwen rows,
OpenRouter GPT-5.6, Vercel AI Gateway and GitHub Copilot GPT-5.4 and 5.5. Cost
estimates for prompts above each threshold previously used the base rate.

An explicit OAuth minimum validity is now a requirement on the refreshed token.
When a provider's tokens are shorter-lived than the requested minimum, the
rotation is persisted and the request fails with an error wrapping
`ErrCredentialUnavailable` instead of silently dispatching a token that expires
too soon and refreshing again on every request.

Anthropic, Codex, and Radius browser logins validate the callback state before
anything else. Requests without the login's state, non-GET requests, and
callbacks without a code are answered with an error page while the login keeps
waiting, so a stale tab or cross-site request can no longer abort it. A
state-matching provider `error` still ends the login, and Codex now reports it
as an authorization failure.

Anthropic browser login falls back to a free loopback port when port 53692 is
reserved or in use, for example by Hyper-V/WSL port exclusions, and then to
pasted input alone. Codex browser login completes with `OnManualCode` input when
port 1455 is held by the Codex CLI. Both previously failed with `address already
in use`.

`ValidateToolCall` accepts schema shapes common in zod, Pydantic, and MCP tool
definitions. ECMAScript `\uXXXX` pattern escapes are translated for Go's RE2
engine, lookaround and backreference patterns are treated as annotations,
array-form tuple `items` with `additionalItems` are validated by position, and
`patternProperties` keys are validated and allowed under
`additionalProperties: false`. These shapes previously rejected every call as a
malformed schema; invalid regexes are still rejected.

Final streamed tool-call arguments containing invalid escapes or raw control
characters inside strings are now repaired for every adapter, matching the
existing Anthropic behavior. They previously reached callers as raw strings and
failed tool validation. Truncated arguments remain unrepaired text.

Mistral tool results keep their string `function.result` shape, but images now
follow the consecutive results as `image_url` chunks in a user entry rather than
inlined base64 text that the model could not see and that inflated the prompt.
Failed tool results are prefixed with `[tool error]`, image-only results read
`(see attached image)`, and empty results send `(no tool output)`.

Anthropic streams that end with an unrecognised stop reason now return a typed
provider error, as the Bedrock, Responses, and Chat Completions adapters do.
`model_context_window_exceeded` is reported as a context overflow, so overflow
handling and route fallback apply where it previously looked like a normal
completion.

Anthropic streams now fail with a typed provider error when a server-side model
fallback starts after output has been produced; previously the refusing model's
partial text and the fallback model's answer were returned as one successful
turn. A fallback before any output remains transparent.

Chat Completions thinking now records the field it streamed in, and same-model
replay sends it back in that field when no reasoning details are preserved.
Thinking tool loops on Moonshot Kimi and similar models previously omitted the
`reasoning_content` those providers require. Older saved blocks without the
recorded field replay as before.

OpenRouter routing options now apply only to requests sent to OpenRouter,
including custom provider IDs that use an OpenRouter base URL. A client-wide
OpenRouter routing default previously added an OpenRouter `provider` field to
every Chat Completions request, which providers such as OpenAI reject.

gpt-oss requests on Groq, Cerebras, Cloudflare Workers AI, and Together now send
`reasoning_effort` for the requested level instead of silently dropping it.
Catalog validation rejects unknown Chat Completions reasoning formats, such as
the `openai` value that disabled reasoning on the Together row.

Bedrock Converse now sends reasoning effort to OpenAI models: gpt-oss receives
`reasoning_effort` clamped to low through high, other GPT models nested
`reasoning.effort`, and `minimal` is sent as low. Levels were previously
dropped, and thinking budgets no longer send an Anthropic `thinking` block to GPT
models. The generated Bedrock gpt-oss and GPT-5.x rows now advertise reasoning,
and the GPT-5.x rows accept `xhigh`, so built-in models can use these levels.

Bedrock Claude adaptive thinking for Opus 4.7 and later, Sonnet 5, Haiku 5, and
Fable 5 now sends `thinking.block_binding` with `drop_block` and the
`thinking-binding-controls-2026-08-01` beta outside GovCloud, matching the direct
Anthropic adapter. Replaying signed thinking after the system prompt or tools
change drops stale blocks instead of failing with an invalid signature.

Claude adaptive thinking on Anthropic and Bedrock sends `xhigh` effort only to
Opus 4.7 and later, Sonnet 5, Haiku 5, and Fable 5, or where model metadata maps
`xhigh` explicitly. Sonnet 4.6 and similar models now receive `high` instead of
an unsupported `xhigh`. Bedrock also uses adaptive thinking for Haiku 5 models.

Turning thinking off on Google and Vertex Gemini models that cannot disable it
now requests the lowest level their metadata supports: `LOW` for Gemini 3.7 and
3.8 Flash, and the minimal budget for Gemini 2.5 Pro. These requests previously
sent `MINIMAL` or a zero budget, which the provider rejects. Models that can
disable thinking are unchanged.

Codex WebSocket requests fall back to SSE only when the connection fails before
any output, or when a connection-limit or missing-continuation error persists
after its retry. Provider error frames, failed responses, and authentication or
payload errors are returned with diagnostics instead of resending the request
over SSE and switching the session to SSE.

Responses stream `error` events with OpenAI's documented top-level `code` and
`message` fields now return typed provider errors over SSE and Codex WebSocket.
Context-length errors are recognized as overflow and server errors as transient,
where they previously surfaced as a generic stream error.

Mistral conversations that end with `stop_reason: "error"`, which Mistral uses
for transient server failures, now return a transient `server_error` with retry
advice. Unknown stop reasons remain non-retryable errors without the event type
as their provider code.

Stream failures without a typed network cause are now classified from their
message. HTTP/2 stream resets and GOAWAY closures while reading a response body
are transient with retry advice instead of non-retryable provider errors.

Context-overflow classification recognizes z.ai CN "Prompt exceeds max length",
DashScope/Qwen "Range of input length should be", DS4 "configured context size",
and generic "too many tokens" errors. Rate-limit wording such as Bedrock
throttling remains excluded.

Temporary provider capacity errors now classify as transient with retry advice:
`server_busy` codes, "servers are currently busy", "Selected model is at
capacity", and Azure "currently experiencing high demand" peak-load rejections,
including the Azure form reported under a generic invalid-request code.

OpenRouter image generation now requests text output only from models that
produce it. Generated metadata records `outputModalities` for all 50 OpenRouter
image models; the 39 image-only models, including FLUX, Recraft, and Seedream,
receive `modalities: ["image"]` instead of a combination OpenRouter rejects.
Caller-registered models without that metadata and explicit `modalities`
options keep their existing behavior.

GPT-5.6 Luna, Terra, and Sol estimates on Azure now match direct OpenAI pricing,
and the Bedrock rows use the 1.1x in-region rate with a 1.05M context window and
272K long-context tier. Bedrock `au.anthropic.claude-opus-4-6-v1` now carries
Opus 4.6 pricing. These rows previously overestimated cost by up to 5x; no
adapters or live-validation claims change.

Responses replay sends a thinking block as a reasoning item only when OpenAI can
resolve it: with same-model encrypted content, or by its provider item ID when
the request sets the `store` provider option. Reasoning models used without a
reasoning level, foreign thinking, and blocks without IDs previously produced
reasoning items that `store: false` requests reject. Saved histories are
unchanged.

Responses streams that omit `output_index`, such as llama.cpp, keep each output
item separate. Previously every event mapped to the first slot, so parallel tool
calls could return with another call's ID, name, or arguments. A tool call that
reuses another call's output index now ends the stream with a typed provider
error instead of overwriting it.

Anthropic message-anchored tool loading now places `tool_reference` blocks
inside the content of the `tool_result` that loads them, as the Messages API
requires. The tool's original output follows every tool result as sibling
blocks, so the conversation cache marker lands on that output rather than on a
reference.

Chat Completions streams no longer start a text block for an empty `content`
delta. Servers such as vLLM and OpenRouter open with `{"content":""}`, which
previously left an empty text block that made tool-only turns fail persistence
validation and placed reasoning after the answer.

Context-overflow messages reported under generic invalid-request codes, such as
Anthropic `prompt is too long` and Bedrock `Input is too long for requested
model`, now classify as context overflow with split-recoverable advice.
`IsContextOverflow` applies the same precedence to final-message diagnostics and
recognizes overflow already detected by provider adapters. Other invalid-request
errors keep their existing classification.

A started OAuth refresh now completes and persists its result even when the
requesting caller is canceled. Stored-credential refresh and the Codex,
Anthropic, GitHub Copilot, Kimi, xAI, and Radius token providers bound the
refresh request and persistence callback by a 15-second timeout instead of the
caller's context, so providers that rotate refresh tokens no longer leave the
only valid token unsaved. Cancellation before or during the ownership wait is
unchanged.

OpenAI Codex requests now use the ChatGPT Codex Responses endpoint over SSE and
WebSocket. Generated Codex models previously posted to `/backend-api/responses`;
base URLs ending in the backend root, `/codex`, or `/codex/responses` now all
resolve to `/codex/responses`. Explicit `endpoint` provider options are unchanged.

Google streams preserve explicitly empty signed text after thinking or tool
calls, so Gemini and Vertex replay signatures on their original blocks. Exact
provenance rules and caller-owned histories are unchanged. Generic stream error
wrappers retain credential failures as authentication errors and retryable
network failures as transient errors, restoring routing advice without automatic
request replay. Synthesized cancellation finals retain provider/model identity
before any event, after partial output, and during independent collector
cancellation. Initial pending snapshots and accepted terminals are unchanged.

Embedding adapters validate consistent nonempty dimensions against the final
payload sent by the responding HTTP attempt, including existing overrides.
Malformed controls fail locally; malformed successes retain typed provider
errors and attempt metadata with no vectors, retries, splitting, or cache writes
from that response. Shared batch checks also cover custom providers, split
responses, and cache-hit combinations. Cache keys advance to version 3: old
entries remain stored but are bypassed, so subsequent requests may regenerate
embeddings and incur provider costs. The image comparison example now uses a
registered model ID. Catalog contents and live-validation claims are unchanged.

Bedrock reasoning replay now omits signatures for non-Claude models and converts
unsigned Claude thinking to ordinary text. Signed and redacted Claude blocks and
persisted histories retain their existing behavior. Manual Claude thinking
budgets respect the effective output cap with 1,024 tokens reserved for output;
thinking is disabled when a minimum 1,024-token budget cannot fit. Enabled manual
thinking resolves an absent output cap from model metadata, falling back to
1,024. Interleaved thinking with tools in the final request retains the exception
allowing a larger budget. Adaptive and non-Claude controls remain unchanged.

Anthropic server-tool invocation counts now remain in raw usage without being
added to token totals. Raw usage accumulates supplied fields across sparse stream
updates, preserves unknown metadata, and applies explicit zero/null replacements
without mutating earlier snapshots. Token totals may therefore decrease for
responses using server tools, while invocation counts remain available in
`Usage.Raw.server_tool_use`. The stored-auth example now correctly wraps its
registry in `WithRegistry`. These corrections add no public APIs or persistence
migrations and are covered by deterministic tests, without live-provider claims.

Responses replay now generates distinct item IDs for parallel tool calls and
reasoning blocks, reserving native IDs before allocating synthetic ones. A shared
request mapping keeps normalized function/custom calls and results associated,
including punctuated, long, and compound IDs. Wire IDs can change when repair is
needed; persisted histories retain their original identities and opaque fields.
Signatures and encrypted content are sent only for matching nonempty
provider/API/model provenance across OpenAI, Azure, and Codex Responses routes.

OpenAI, Gemini, Vertex, and Bedrock embeddings accept successful response bodies
up to 256 MiB instead of silently truncating at 16 MiB. Oversized successes return
explicit typed provider errors with status, request identity, and attempts;
they are not retried, partially returned, or cached. Existing batch-splitting and
vector-validation rules continue to apply.

Native Vertex text, images, and embeddings and OpenAI/Anthropic MaaS routes now
apply resolved routing, headers, and provider options before constructing each
HTTP attempt. Caller overrides, credential modes, token-provider precedence,
cancellation, and final header suppression remain in effect. Retries obtain fresh
auth defaults. These corrections add no public API or persistence migration.

Bedrock requests signed with AWS access keys now double-encode the canonical
request path as SigV4 requires. Model IDs containing `:`, such as `…-v1:0`,
and inference-profile ARNs previously produced signatures that AWS rejected.
Bearer-token requests are unchanged.

On models that advertise thinking support, `ThinkingLevelMap` now overrides
provider values rather than listing every supported level. Unmapped levels
through `high` stay supported and are sent as the level text; `xhigh` and `max`
still require an entry, and `UnsupportedThinkingLevels` rejects a level. This
restores low through high reasoning on generated GPT-5.x rows. Models without
`SupportsThinking` keep the previous behavior, where the map lists every
supported level.

The Claude Code identity is now limited to Anthropic subscription tokens. GitHub
Copilot and Kimi Coding OAuth credentials on Anthropic Messages routes no longer
receive the Claude Code system prompt, betas, or client headers.

The September model registry review adds 50 text models across existing routes,
GPT Image 1.5 and 2, and text input for Gemini Embedding 2. It updates 18 existing
text rows and removes 167 text entries, three OpenRouter image previews, and
direct Gemini `text-embedding-004`. That review produced 548 text,
56 image, and 8 embedding models. Removals include IDs absent from the comparison
inventory across matching providers and confirmed direct-provider shutdowns.
Removed IDs no longer resolve through default discovery; applications must
explicitly choose replacements. Histories and stored embeddings are unchanged.

The October Vertex review brings its catalog to 39 text models (14 native
Gemini, 17 Claude, and eight OpenAI-compatible MaaS), three Gemini image models,
and one embedding model. The complete offline catalog now contains 562 text,
58 image, and 8 embedding models. Additions use existing adapters; model and
region access still depend on the caller's account. No runtime route or release
classification changes, and live validation remains opt-in.

Vertex Gemini Pro estimates now apply documented pricing above 200,000 input
tokens, including cached input. Newer Claude models retain flat global rates
across that boundary. Claude Sonnet 4.6's output limit, Llama 3.3's pricing and
output limit, and Gemini image aspect ratios are corrected. Deprecated models
with future retirement dates remain discoverable. Existing restrictions on
named thinking levels for Vertex latest aliases remain in place; Grok and GLM
MaaS entries preserve provider-default reasoning without advertising named
controls that the adapter cannot express.

Vertex embeddings add `gemini-embedding-001`, with default batching limited to
one input per request. Default Vertex discovery excludes
`gemini-3.1-flash-lite-image`, `text-embedding-004`, `text-embedding-005`, and
`text-multilingual-embedding-002` because they are absent from the reviewed
inventory. These are catalog-policy exclusions, not provider-retirement claims.
Text models represented by equivalent versioned Claude IDs remain discoverable.
Model selection and vector-store migration remain explicit; stored vectors,
histories, caller-registered metadata, and other providers are unchanged.
Image cost metadata records baseline output prices; larger resolutions can cost
more. Deterministic tests cover routing, reasoning payloads, batch splitting,
pricing boundaries, and removal boundaries.

Pricing remains provider-specific and estimated. New entries preserve existing
route, authentication, context-default, and transport policies. Google Flash
introductory pricing and upcoming model retirements require follow-up before
their published deadlines.

Synchronous provider errors now redact recognized credentials in their displayed
messages while retaining the original causes for `errors.Is` and `errors.As`.
Responses results preserve all output-text and refusal parts in content-index
order, including final-only and deferred responses, with one public text block
per output item. Terminal snapshots no longer replace the message with its last
part or duplicate accumulated text.

All built-in embedding decoders reject absent, null, empty, non-array, or
malformed vectors and numbers that overflow float32. Valid zero-valued vectors
remain supported. Malformed HTTP successes return typed provider errors with
status, request ID, model identity, and attempts, without partial vectors or
cache writes. Existing dimension policies and cache identities are unchanged.

OAuth callers canceled while waiting for another caller's refresh now return
promptly. Credential inspection, rotation, and persistence callbacks remain
serialized. Strict tool validation preserves valid explicit nulls, including
unconstrained optional properties, and removes optional null placeholders only
when the original schema rejects null. Combined type, enum, const, and supported
union constraints determine strict-schema nullability; malformed-schema errors
remain errors. These changes require no persistence migration.

Shared copies now preserve nil lists as `null` and non-nil empty lists as `[]`
across tool content, provider options, metadata, and credentials. This corrects
serialization that previously depended on the Go slice type; existing byte and
raw-JSON conventions remain unchanged. Tool validation compares nested numbers
mathematically, so `1`, `1.0`, and `1e0` are equivalent within object and array
`const`/`enum` constraints, including exclusions through `not`.

Codex WebSocket handshake, proxy, and retained session errors redact recognized
credential material and bound diagnostic previews, including when SSE fallback
succeeds. OpenAI tool-schema conversion, WebSocket requests, and continuation
caches preserve exact schema numbers, and changes to large integer constraints
invalidate continuation reuse. OpenAI embedding successes now require exactly
one explicit, unique, in-range index per input. Malformed responses return typed provider errors with
the original HTTP status and attempt metadata, without partial vectors; valid
out-of-order responses still return in input order. These corrections add no
public APIs, dependencies, catalog changes, or history migrations.

Tool replay now preserves empty argument objects throughout content cloning and
handoff, including objects nested in arrays. Previously persisted explicit nulls
remain unchanged; no history migration is attempted. Chat Completions uses one
deterministic mapping for function calls, custom calls, and results, preserving
safe IDs while distinguishing composite IDs and long or punctuated IDs. Transformed
wire IDs may change. Developer instructions no longer prematurely close tool
exchanges: repair matches actual results, synthesizes only missing results, then
emits held instructions before role conversion and compatibility bridge insertion.
Handoff reports retain original source indices and final inserted-message indices.

Tool validation now applies supported constraints without a repeated type in
reference siblings and composed schemas. Previously accepted out-of-range
arguments may fail validation. Opt-in coercion resolves nested and recursive local
references against the original root while preserving valid union values and
caller-owned inputs. The comparison guide now reflects supported references,
formats, and conditionals. Shared SSE parsing ignores data-free control frames;
explicit empty data and malformed JSON retain their existing error behavior.
Public APIs, dependencies, persistence formats, catalog data, and provider
capabilities are unchanged by these corrections.

Client-default tool choices now reach provider requests, and incremental tool
events isolate nested arguments and metadata from provider state and final
results. Exact numbers and empty argument objects survive these copies,
including aborted finals. Bedrock now accounts for one-hour cache writes and
rejects missing completion reasons while retaining partial output and trailing
usage. Codex WebSocket proxy exclusions now handle parent domains, wildcard
lists, ports, and IPv6 literals. Documentation also reconciles the existing
credential chain, image adapters, and redacted debug hooks.

Internal evaluations now pair Go subtests by stable case ID and validate
operational requirements before judging. Missing measurements from any model
turn keep the corresponding token or cost aggregate unavailable rather than
reporting misleading subtotals. Artifact schema v2 records immutable inputs and judged outputs,
named judgments, evaluated prompts/tools, and allowlisted controls before
provider request adjustments. Old artifacts are not migrated, iteration
metadata remains v1, and public Sigma APIs are unchanged. Auth timeout tests
now synchronize resolver completion before inspecting counters, eliminating
an intermittent race-detector failure.

Evaluation comparisons now exclude independently failed or skipped tests while
retaining threshold-only scores. Threshold failures are reported during cleanup
and remain separate from CLI operational failures. Submitted table runs that
fail before dispatch retain their comparison identities and diagnostics;
never-submitted runs remain outside this accounting. Numeric tool-event arguments
reach judges as exact `json.Number` values. Artifact and iteration schema
versions remain unchanged. The provider parity guide also now documents the
existing direct OpenAI background submit, single-fetch, and cancel lifecycle
while retaining the deferred boundaries for other routes and orchestration.

Tool arguments now retain exact JSON numbers through persistence and replay on
Anthropic, Google, and Bedrock, including shared Vertex routes. Anthropic hosted
search, fetch, and code-execution results survive in ordered block metadata and
replay only with matching provider/API/model provenance. Old histories remain
readable, but results discarded by earlier versions cannot be reconstructed.

Request preparation removes tool exchanges and opaque reasoning from failed or
aborted assistant turns while retaining nonblank visible text. Responses keeps
its existing whole-turn omission policy. Stored history and partial finals are
unchanged, and handoff preserves the information needed to apply this policy
at dispatch. Anthropic also omits blank text and empty messages, preserves valid
signed thinking and empty tool results, and rejects all-empty requests locally.

Google and Vertex text prompt blocking now terminates as content filtering.
`MALFORMED_FUNCTION_CALL` and `UNEXPECTED_TOOL_CALL` now return typed,
non-retryable provider errors and emit `error` events, with partial content,
usage, and raw finish reasons retained. Callers that previously expected a nil
error for these stop reasons must handle the returned failure.

Google Vertex file-read probes now explicitly request only a `read_file` call
for `README.md`, without asking for the file contents. This removes missing
argument and response-sequencing ambiguity from both automatic and forced tool
cases; provider-generated malformed calls still surface as failures.

Failed xAI JSON-object probes now compare an explicit formatting instruction,
a matching JSON schema, and plain-text mode using the same user prompt, plus
three JSON-object variants with string, number, and alternate boolean values.
All retain the output budget. Successful controls and per-attempt errors provide
evidence for persistent failures while the original safety rejection remains
inconclusive.
An explicit `-json-text` mode runs the same JSON-object prompt in plain-text mode
and validates the completed answer locally. It reports a separate `json_text`
case and does not claim provider-enforced JSON support or downgrade application
requests automatically.

Bedrock stream closure now cancels outstanding work and releases idle transports.
AWS credential/config file overrides replace their respective default paths;
applications relying on fallback to an overridden home-directory file must
configure their intended credential source explicitly. The provider guide now
covers the existing default chain, caching, and resolver-only configuration.
Tool validation now rejects numeric arguments that previously passed because of
floating-point rounding, and decimal-string coercion retains exact values.
Typed JSON-compatible containers are isolated across content, registry, option,
and credential copies; opaque Go objects remain caller-owned. Tool streaming
avoids redundant argument-prefix copies, and weighted embedding combination
avoids overflow from finite coordinates and integer weight totals.

Surface probes now preserve recognized safety rejections as inconclusive
failures. Later successful variants remain diagnostic controls and do not
produce token-budget repair claims or recommendations for those cases.
OpenCode Zen/Go now send caller session IDs as `x-opencode-session` across
routed APIs even when caching is disabled, preserving explicit header overrides
and suppression. Surface probes generate separate conversation IDs per case,
reuse them across turns, retries, and repairs, and report `MissingSessionID` as
a request-shape error.

`sigma` v0.8.0 begins by tightening native Gemini 3 replay compatibility across
Google Generative AI and Vertex AI so function calls and matching tool results
retain stable normalized IDs. Google and Vertex streams now also preserve
explicit max-token, provider-error, and unknown finish reasons when a response
contains function calls, so only a normal `STOP` is promoted to tool-call
completion.
Google and Vertex replay now also retain blank signature-only text and thinking
parts only when the signature is valid for the same provider, API, and model.
Amazon Bedrock Converse Stream service exceptions also retain their requested
model and AWS request ID for diagnostic correlation.
Replayed Bedrock tool inputs now remove provider-rejected empty object-member
names without altering stored or streamed tool arguments.
Bedrock Converse also preserves encrypted reasoning blobs across split stream
deltas and replays them through the provider's scalar base64 wire shape without
exposing opaque reasoning as visible text.
Qwen Token Plan now exposes Qwen3.8 Max under its generally available model ID
across both regional routes while preserving supported reasoning levels and
keeping Qwen3.7 Max toggle-only. A distinct Individual subscription route adds
eight curated models, including DeepSeek V4 Pro 0813, through the shared
international endpoint and credential, with each model's thinking controls
preserved. Z.ai and Z.ai Coding CN now add GLM-5.2 Highspeed and GLM-5.3 with
million-token limits, model-specific reasoning aliases, and evidence-backed
estimated pricing. Enabled Z.ai reasoning requests now also preserve exact
same-provider/API/model reasoning across turns for provider-side caching.
Baseten is now available through a first-class
OpenAI-compatible route with focused GLM 5.2 and Kimi K2.6 metadata and native
chat-template thinking controls. GLM 5.2 now advertises both text and image
input so image-bearing tool results remain available on that route. Xiaomi's
direct and regional Token Plan catalogs now expose only the supported MiMo V2.5
text-provider lineup instead of retired V2 model names.
Curated OpenRouter text routes now carry reviewed optional or mandatory
reasoning behavior and exact effort availability. Optional routes explicitly
send `reasoning.effort: "none"` when reasoning is omitted; mandatory routes
retain provider defaults and reject explicit off or unsupported efforts before
dispatch.
OpenCode Zen and Go now expose the complete current 63- and 27-model
catalogues. The generated refresh adds 26 models, removes ten no longer
advertised, and reconciles routed APIs, inputs, thinking levels, pricing,
cache pricing, and token limits. Known live-probe models now resolve their
route from generated metadata, and region opt-in failures are reported as
upstream availability rather than an inconclusive request-shape failure.
The curated OpenRouter image catalog now also includes Seedream 5.0 Lite and
Pro, Qwen Image 3 and 3 Pro, Meta Muse Image, Grok Imagine Image 2.0, and four
Recraft V4 Styles variants through the existing image adapter. Their estimated
image costs remain zero until independently verified pricing is available.
Fireworks GLM 5.2 routes now use session affinity for automatic
prompt caching without unsupported long-cache retention. Anthropic-routed
OpenRouter agent loops now advance their final conversation cache breakpoint
through the latest non-empty tool result.
OpenCode Zen and Go DeepSeek V4 Flash routes support low reasoning effort while
retaining their high and maximum-effort mappings. The September registry
review removes direct V4 Flash and its experimental vision variant from default
discovery. Direct V4 Pro retains documented peak-rate cost estimates.
Direct xAI now includes Grok 4.6 through OpenAI Responses with text and image
input, function tools, 500k-token context and output limits, tiered
long-context pricing, and reasoning controls through `xhigh`.
Anthropic Messages streams now surface text and thinking delivered with
content-block start events immediately through incremental output. Refusal
stops now also retain non-empty structured provider details for callers that
need the refusal category or explanation. Direct Claude Fable 5 requests can
additionally opt into catalog-declared server-side refusal fallbacks without
changing default requests; known fallback responses retain the requested model
identity while reporting usage and estimated cost against the returned model.
Direct Claude Opus 5 now preserves the provider-native thinking effort on
partial and terminal assistant messages and replays it for exact matching prior
turns, preventing signed-thinking mismatches when effort changes between turns.
In-progress text streams now identify every partial assistant snapshot as
pending, beginning with an empty snapshot on the initial start event.
OpenAI-compatible Chat Completions models can also opt into successful
`[DONE]` termination when their endpoint does not emit `finish_reason`.
Indexed OpenAI-compatible Chat Completions tool-call continuations now retain
the first provider-issued ID and name across mixed text, reasoning, and tool
deltas, preventing identity drift during execution, persistence, and replay.
OpenAI-compatible Chat Completions streams now also retain validated encrypted,
signed-text, and summary reasoning details in their original order across
assistant-content persistence and exact same-provider/API/model replay,
including responses without tool calls.
OpenAI-compatible Chat Completions, Responses, and Azure Responses requests can
now carry request-scoped arbitrary sampling parameters with explicit override
precedence. Caller-registered models can also declare default arbitrary
sampling fields that remain below Sigma's core and typed request values,
request-scoped sampling overrides, and raw provider body overrides. OpenAI
Responses-compatible and Azure OpenAI Responses requests now also clamp typed
output-token limits below 16 to the accepted request minimum.
Models can now opt out of that automatic output-token field through
`OpenAIResponsesCompat.SupportsMaxOutputTokens`. Cache-options-capable
Responses models now also map automatic long retention to
`prompt_cache_options.ttl: "30m"`, including the existing direct GPT-5.6 rows.
Direct OpenAI Responses requests can now run in the background through an
explicit provider-neutral lifecycle. Callers receive a JSON-serializable,
provenance-bearing handle, can perform one status fetch at a time, and can
request cancellation without changing ordinary streaming or completion
behavior. Completed and incomplete background responses use the same content,
tool, usage, cost, metadata, and error conversion as streamed Responses.
OpenAI-compatible Chat Completions usage now also recognizes top-level
`cached_tokens` from compatible Kimi and Moonshot responses as cache reads
instead of ordinary input.
Custom OpenAI-compatible Chat Completions models can also opt into safely
clamped top-level thinking-token budgets for compatible inference servers.
Text, image, and embedding requests can now ask stored credentials and built-in
caller-owned OAuth token providers to refresh before dispatch when too little
token lifetime remains, without changing existing refresh defaults.
Provider OAuth descriptors and registry summaries now also identify known
subscription-backed flows so applications can distinguish them from generic
OAuth sign-in without inferring from provider names or credential types.
GitHub Copilot callers can also discover the authenticated account's available
model IDs and filter Sigma's curated catalog without enabling policies or
mutating registry state. Explicit model-policy enablement now retries throttled
requests within a five-second bound while remaining caller-invoked.
Overlapping runtime text, image, and embedding model-source operations now
publish per-provider registry state in latest-started order, so a slower older
refresh cannot overwrite a newer refresh or cached text restore.
Kimi and Kimi Coding requests now use a Sigma-owned coding-endpoint identity
from their shared provider wrapper instead of duplicated model-catalog headers.
The opt-in evaluation runner now isolates every case/model/repetition run with
an independent deadline so a stalled provider call does not cancel later
evaluations. Repository-internal Sigma evaluation harnesses can now execute
bounded caller-owned text tool loops, and the live suite adds a deterministic
tool-call round trip that verifies the call, local result, and final answer.
The opt-in surface probe now also exercises Anthropic Claude through Vertex
`streamRawPredict`, using catalog-backed model selection and the existing
explicit Vertex routing and credential contract.
Image probe mode now also exercises the Google Gemini API and Vertex AI image
adapters with generated Gemini 2.5 and 3.1 image metadata. Explicit model
selections are validated locally, every image case has an independent deadline,
and success requires non-empty base64 or URL image data.
Reviewed OpenAI Responses and Codex Responses models now use native,
message-anchored additional-tool input, and streamed tool-call namespaces are
retained only when their loading context can be replayed safely. Codex Responses
SSE and WebSocket streams also recognize `response.done` completion and retain
explicit `end_turn` values as opaque diagnostics. OpenAI, Azure, and Codex
Responses streams now also retain non-empty assistant message phases through
persistence, with recognized commentary and final-answer boundaries replayed
only to the exact provider, API, and model. Later Responses requests now also
omit failed or aborted assistant turns and their associated tool results from
wire history, avoiding incomplete reasoning and call pairings while retaining
the partial finals locally for callers. Provider failures that report an
exhausted upstream request buffer are now classified as retryable for
caller-owned recovery. Responses incomplete terminals now distinguish
max-output and content-filter stops from missing or unknown reasons, with a
provider-neutral helper for bounded caller-owned max-token recovery. Existing
strict function-tool opt-ins now derive provider-compatible closed schemas for
supported OpenAI-compatible Chat Completions, Responses, Mistral, and
capability-gated Anthropic Messages routes, while local validation maps
optional non-nullable `null` placeholders back to omission without mutating
caller-owned data.
Text requests can now select automatic or disabled tool use through one
provider-neutral option across every built-in text API, while advanced tool
selection remains available through existing provider-specific controls.

## Added

- The November 22 catalog review adds 75 text models through existing
  adapters. Each new row takes its name, input types, limits, and prices from
  the models.dev snapshot, and its thinking, compatibility, auth, and transport
  metadata from the closest reviewed sibling, then adjusts thinking levels to
  the model's own controls:
  - Anthropic: Claude Sonnet 5.5 and Fable 5.2 reject disabled thinking like
    Opus 5.5 and Fable 5.1; Claude Haiku 5.5 accepts it like Opus 5. Haiku 5.5
    prices requests above 100,000 input tokens at its long-context tier.
  - Bedrock: Claude Sonnet 5.5, Opus 5.5, Haiku 5.5, Fable 5.1, and Fable 5.2
    on the in-region, geographic, and global routes the snapshot lists, plus
    the US, EU, AU, and JP Opus 5 profiles. Geographic profiles cost 1.1 times
    the global rate, and in-region IDs carry the global rate like existing
    rows. The plain `anthropic.claude-opus-5` ID stays excluded. GPT-6, 6.1,
    and 6.2 arrive on the in-region, US, and global routes at the snapshot's
    prices.
  - OpenAI: GPT-6.1 Sol, GPT-6.1 Terra, GPT-6.2 Astra, and GPT-6.2 Sol keep the
    272,000-token context default and long-context price tier; only GPT-6.1
    Terra accepts a `none` effort for thinking off.
  - Gemini and Vertex: Gemini 4 Argon, Carbon, Hydrogen, and Neon support low,
    medium, and high thinking, since Gemini has no xhigh level to send.
    Gemini Pro (latest) follows Gemini 3.1 Pro Preview. Vertex adds Claude
    Haiku 5.5 and Fable 5.2.
  - Others: DeepSeek Flash; Mistral Large 4 with low, medium, and high effort
    and no off; Qwen3.8 27B on Groq and Cerebras with a `none` off effort; and
    NVIDIA NIM DeepSeek V4.1 Flash, Nemotron 3.5 Super VL and Lightning,
    MiniMax M3.1 Flash, and GLM-5.3 Flash.

  The review also raises GPT-5 Pro's output limit to 272,000 tokens, prices
  `gemini-flash-latest` and `gemini-flash-lite-latest` as Gemini 3.8 Flash and
  3.5 Flash Lite, which they now resolve to, and moves OpenRouter GPT-5.6 Sol
  from its launch price to the 4/20 rate the direct, Azure, and Bedrock rows
  already use. Other snapshot differences, including bulk OpenRouter rows,
  realtime and research models, and unverified price or limit changes, were
  not imported. Catalog inclusion does not guarantee account access.

- Direct OpenAI and OpenAI Codex now include `gpt-6-astra` through their
  existing Responses adapters, with text/image input, function tools,
  low/medium/high/xhigh/max reasoning, and message-anchored deferred tools.
  Explicit off and minimal reasoning are rejected locally. Direct OpenAI
  supports explicit prompt-cache mode and 30-minute long retention; Codex
  keeps its existing cache behavior and output-token field omission.
  Both catalog rows use Sigma's conservative 272,000-token context default
  and a 128,000-token output limit, although the documented API context
  maximum is 1,050,000 tokens. USD estimates per million tokens are
  10 input, 50 output, 1 cache read, and 12.5 cache write; requests above
  272,000 input tokens use 20/75/2/25 for the full request. Codex costs are
  API-equivalent estimates, not subscription charges. Catalog inclusion does
  not guarantee account access. See the
  [model documentation](https://developers.openai.com/api/docs/models/gpt-6-astra).
  Provider maturity, existing models, and dispatch defaults are unchanged;
  this addition does not introduce async tools or mid-turn steering.

- OpenRouter image generation now exposes Seedream 5.0 Lite and Pro, Qwen
  Image 3 and 3 Pro, Meta Muse Image, Grok Imagine Image 2.0, and Recraft V4
  Styles, Styles Pro, Styles Vector, and Styles Pro Vector through the existing
  image route. Each row retains the shared OpenRouter endpoint, authentication,
  1024x1024 PNG capability contract, and a zero estimated image cost pending
  independently verified pricing.
- `SubmitDeferred`, `FetchDeferred`, and `CancelDeferred` provide an explicit
  provider-neutral lifecycle for durable text responses, initially backed by
  direct OpenAI Responses. `DeferredResponseHandle` is safe to serialize for
  later polling and pins the provider, API, model, and response ID so a handle
  cannot be dispatched to a different registered route. Queued and in-progress
  observations carry no assistant message; terminal output reuses the existing
  Responses parser, including reasoning, function and grammar tools, usage,
  estimated cost, routed-model metadata, and partial output on failures.
  Fetching performs exactly one request and cancellation returns the resulting
  provider status, including an already-completed response. Sigma does not
  automatically poll, resume background streams, or extend upstream retention;
  callers using the default non-stored request policy must retrieve results
  within the provider's temporary polling window.
- The international and China Z.ai Coding Plan routes now expose GLM-5.2,
  GLM-5.2 Highspeed, and GLM-5.3 as a consistent million-token cohort. GLM-5.2
  variants map Sigma's `minimal` through `high` levels to provider `high` and
  `xhigh`/`max` to provider `max`; GLM-5.3 maps `minimal`/`low` to `low`,
  `medium`/`high` to `high`, and `xhigh`/`max` to `max`. Omitted reasoning
  remains disabled, and explicit GLM-5.3 `off` fails locally. The China route
  additionally exposes GLM-4.6V with text and image input, Z.ai reasoning,
  streamed tools, a 128,000-token context window, a 32,768-token output limit,
  and `max_tokens` request compatibility. GLM-5.1, GLM-5.2, and GLM-5V-Turbo
  use API-equivalent estimated pricing across both routes, while unevidenced
  prices remain zero. The typed Z.ai reasoning format now defaults
  same-provider/API/model assistant replay to `reasoning_content` and sends
  `clear_thinking: false` whenever reasoning is enabled so provider-side
  reasoning state remains cacheable. An explicit
  `RequiresReasoningContentOnAssistantMessages` setting overrides the replay
  default; mismatched provenance is never replayed, and disabled or omitted
  reasoning retains the existing disabled payload without `clear_thinking`.
- `AnthropicOptions.EnableRefusalFallbacks` enables direct Anthropic
  server-side refusal fallback only for models with generated allowed-target
  metadata. Claude Fable 5 uses the ordered Opus 4.8 and Opus 5 targets.
  Disabled requests omit both the fallback payload and beta header; unsupported
  opt-ins fail before dispatch. Provider-reported declared fallback models
  drive usage identity and estimated pricing, while unknown returned model IDs
  remain diagnostic and retain requested-model accounting.
- `AnthropicMessagesCompat.SupportsMidConversationEffort` capability-gates
  provider-native thinking-effort persistence and replay. Direct Claude Opus 5
  records the resolved effort in `AssistantMessage.ProviderThinkingLevel` and
  persisted `Message` values, emits it on partial and terminal results, and
  inserts exact-provenance replay markers before matching assistant turns. An
  omitted reasoning level defaults to `high`. Explicit off disables thinking
  without effort markers on Opus 5 and is rejected where metadata marks it
  unsupported, such as Opus 5.5 and Fable 5.1.
- `WithToolChoice` accepts `ToolChoiceAuto` or `ToolChoiceNone` across OpenAI
  Chat Completions, Responses, Azure Responses, Codex Responses, Anthropic
  Messages, Google Gemini and Vertex, Mistral Conversations, and Bedrock
  Converse. Required, any, named-tool, and custom configurations remain on the
  existing provider-specific option surfaces.
- Google Generative AI and Vertex assistant replay now preserve blank text and
  thinking parts when their thought signature passes the existing
  provider/API/model and base64 checks. Unsigned whitespace and blank parts with
  invalid or foreign signatures are omitted, while nonblank content and tool
  calls retain their original order.
- `StopReasonPending` now identifies in-progress assistant output. Every
  non-terminal text event carries a `PartialMessage`; the initial start event
  receives an empty pending snapshot, and accumulated content snapshots remain
  pending until a provider supplies another explicit reason or terminates the
  stream.
- Strict function tools now receive derived provider schemas with closed
  objects, all properties required, and originally optional non-nullable
  properties represented as nullable. The existing boolean
  `Tool.ProviderMetadata["strict"]` opt-in drives this behavior on supported
  OpenAI-compatible Chat Completions, Responses, Mistral Conversations, and
  capability-gated Anthropic Messages routes. Built-in direct Anthropic models
  advertise support through `AnthropicMessagesCompat.SupportsStrictTools`, and
  `anthropic.MessagesCompat.StrictTools` lets custom endpoints override the
  detected capability. `ValidateToolCall` removes matching provider-emitted
  `null` placeholders from its decoded argument copy.
- `OAuthAuth.IsSubscription` and `ProviderAuthInfo.OAuthSubscription` expose
  advisory subscription metadata through detailed provider auth descriptors,
  registry listings, and snapshots. Anthropic, GitHub Copilot, Kimi Coding,
  OpenAI Codex, and xAI mark their OAuth flows as subscription-backed; Radius
  and unmarked custom OAuth flows retain the zero-value generic classification.
- `githubcopilot.DiscoverGitHubCopilotModels` returns the authenticated
  account's available model IDs, and `GitHubCopilotModelAvailability.ModelFilter`
  creates a snapshot filter for `Client.Models`. Discovery excludes explicitly
  tool-incompatible models and recovers Individual account availability from
  enabled policies only when the picker catalog is empty.
- Runtime text, image, and embedding model-source publication now uses
  independent per-provider latest-started generations. Text refresh and cached
  restore operations share the same ordering rule, while operations for other
  providers continue independently.
- `IsRecoverableMaxTokens` reports when a max-token completion used fewer
  output tokens than the caller's original requested or model limit. It is
  advisory only; callers retain control of compaction, retry budgets, and
  request replay.
- The reviewed GPT-5.4, GPT-5.4 Mini/Pro, GPT-5.5, and GPT-5.6 OpenAI Responses
  rows plus GPT-5.6 Codex Responses rows now encode deferred client tools as
  native developer-role `additional_tools` items immediately after the matching
  tool result. Older tool-search-capable models retain the existing paired
  client search replay.
- `WithOAuthMinimumValidity`, `WithImageOAuthMinimumValidity`, and
  `WithEmbeddingOAuthMinimumValidity` now let callers require additional OAuth
  lifetime before dispatch. Stored credentials refresh serially and persist
  rotations, while built-in caller-owned token providers update their in-memory
  credentials through the existing refresh callbacks.
- `OpenAIOptions.SamplingParameters` now carries arbitrary request-scoped
  sampling fields for OpenAI-compatible Chat Completions, Responses, and Azure
  Responses. These fields override typed request values, while raw provider
  `extra_body` values retain final precedence; unsupported APIs reject non-empty
  sampling maps before dispatch.
- `OpenAICompatibleModelConfig.SamplingParameters` and
  `MetadataOpenAISamplingParameters` now let caller-registered models provide
  default arbitrary sampling fields for OpenAI-compatible Chat Completions,
  Responses, and Azure Responses. Required payload fields and typed request
  options override model defaults, request-scoped sampling maps override
  matching keys while retaining other defaults, and raw provider `extra_body`
  values retain final precedence.
- `OpenAICompletionsCompat.ThinkingTokenBudgetField` now lets custom compatible
  models select top-level `thinking_token_budget`, `thinking_budget`, or
  `thinking_budget_tokens` payloads when callers select reasoning and provide
  an explicit positive budget. `SupportsThinkingTokenBudget` remains a
  backward-compatible alias for `thinking_token_budget`, while an explicit
  field selector takes precedence. Sigma clamps the budget against the request
  or model output ceiling to preserve 1,024 tokens for visible output;
  sampling parameters and raw `extra_body` values retain their existing
  override precedence.
- `cmd/sigma-evals-runner` now defaults each case/model/repetition run to an
  independent one-minute timeout bounded by the overall command deadline.
  Timed-out runs remain operational failures with partial artifacts and
  comparison diagnostics, while later runs continue when the overall deadline
  remains active; callers can configure the duration or disable it.
- Repository-internal Sigma evaluation harnesses can now execute caller-owned
  text tools across bounded completion rounds. Successful and intentional-error
  tool results retain names in replay, normalized traces, accumulated usage,
  and private transcripts; executor failures remain operational errors. The
  opt-in live runner now includes a sixth case whose hidden local result passes
  only with a matching successful tool call/result trace and exact final answer.
- `cmd/sigma-surface-probe` now exposes `google-vertex-anthropic` as an opt-in
  live route over the existing Vertex Anthropic provider. The route defaults to
  the built-in `claude-sonnet-4-6` row, validates explicit Claude IDs against
  Sigma's catalog without model discovery, and capability-gates image,
  reasoning, and tool cases from the selected model metadata. Anthropic tool
  cases use provider-neutral automatic choice or typed required choice instead
  of OpenAI-specific request options.
- `cmd/sigma-surface-probe -images` now exposes opt-in `google` and
  `google-vertex` routes. The direct route probes `gemini-2.5-flash-image` and
  `gemini-3.1-flash-image` through `generateContent`; the Vertex route probes
  the generated Gemini 3.1 row through Vertex `generateContent`. Direct
  credentials prefer `GOOGLE_API_KEY` over `GOOGLE_CLOUD_API_KEY`, while Vertex
  reuses the existing explicit project, location, OAuth-token, and API-key
  contract. Each case has an independent timeout bounded by the overall probe
  deadline, and successful responses must contain a non-empty base64 or URL
  image.
- Persisted tool-result messages can now retain optional `Usage` from
  caller-owned tool execution through replay and model handoff. This metadata
  is not serialized into provider requests and remains separate from
  assistant-usage context anchors, model-turn cost accounting, and evaluation
  usage totals.
- `OpenAICompletionsCompat` now supports an opt-in setting for endpoints that
  end streams with `[DONE]` but do not emit `finish_reason`.
- Qwen Token Plan Individual now provides a distinct registration route for
  DeepSeek V4 Flash 0731, DeepSeek V4 Pro, DeepSeek V4 Pro 0813, GLM-5.2,
  Qwen3.6 Flash, Qwen3.7 Max, Qwen3.7 Plus, and Qwen3.8 Max. It reuses the
  international endpoint, `QWEN_TOKEN_PLAN_API_KEY`, and the shared
  OpenAI-compatible Chat Completions adapter.
- Baseten now provides a first-class registration route backed by the shared
  OpenAI-compatible Chat Completions adapter. The focused built-in catalog
  covers vision-capable GLM 5.2 and Kimi K2.6 with `BASETEN_API_KEY` discovery,
  reviewed inputs, limits, and token pricing.
- Direct DeepSeek retains V4 Pro with peak estimates of $1.32 input, $3.96
  output, and $0.044 cached input per million tokens. The September review
  removes direct V4 Flash and V4 Flash Vision Exp from default discovery.
  Caller-defined vision metadata still uses the existing Chat Completions adapter.
- Xiaomi's direct catalog provides `mimo-v2.5`, `mimo-v2.5-pro`, and
  `mimo-v2.5-pro-ultraspeed`. The regional Token Plan catalogs retain the first
  two IDs through their existing OpenAI-compatible Chat Completions routes.
- Direct xAI metadata now includes Grok 4.6 through the existing OpenAI
  Responses registration path. It accepts text and image input, function
  tools, and low, medium, high, or `xhigh` reasoning within a 500k-token
  context and output limit. Standard rates are $2 input, $6 output, and $0.50
  cached input per million tokens; requests above 200k input tokens use the
  $4, $12, and $1 rates.

## Reliability corrections

- Preserve supplied Responses cache-write usage in normalized and raw accounting,
  deduct cache reads and writes from ordinary input, and apply existing pricing
  and tier adjustments across streaming and deferred results, Azure, and Codex.
- Keep distinct adjacent reasoning blocks separate when IDs, formats, signatures,
  or supplied indexes conflict. Compatible fragments still merge, missing identity
  fields may be filled, and encrypted entries remain discrete and ordered.
- Accept OpenRouter image response bodies up to 64 MiB, with typed, safe overflow
  errors retaining HTTP status and request ID and no retry after body consumption.
- Apply schema container keywords to the actual value without inferring a type;
  preserve explicit types, nested opt-in coercion, full-schema validation, exact
  numbers, strict nullable semantics, and caller-owned data.
- Require exact nonempty provider/API/model provenance for modern and legacy
  reasoning replay. Stored histories remain readable and unchanged; incompatible
  reasoning is omitted while ordinary text, tool calls, and results are retained.
- Reconcile Azure, Codex, and Xiaomi provider documentation with registration
  helpers, generated metadata, auth paths, and Xiaomi regional token-plan routes.

Synthetic tool-call IDs for Google and OpenAI Chat Completions are now random,
opaque identifiers that remain distinct across turns and concurrent streams.
This includes custom Chat Completions tools and Vertex text through the shared
Google parser. Provider-authored IDs retain their existing behavior; already-invalid
persisted histories with duplicate IDs are not automatically migrated.

Google text, images, and embeddings and OpenRouter images now resolve rich auth
before building each request's route, payload, and headers. Each retry starts
from caller options and refreshes auth defaults once. Caller overrides, protected
credential headers, final header suppression, and request timeouts retain their
existing contracts. HTTP-client selection and embedding-cache namespaces remain
caller-owned.

OpenAI and OpenRouter inline image errors redact messages, codes, and error-type
metadata in structured results, including serialized results. Direct Google and
Vertex Gemini image results retain candidate finish reasons and prompt feedback
in metadata. Safety and token-limit stops preserve partial output without automatic
retry; explicit error reasons produce typed provider errors, and empty envelopes
without terminal evidence are rejected. Imagen's separate protocol is unchanged.

Oversized-input embedding batching now computes weighted averages in float64
before returning float32 coordinates, avoiding overflow from large finite inputs.
The result remains unnormalized. These corrections add no exported APIs,
dependencies, persistence schema changes, catalog changes, or provider promotions.

Tool-argument decoding preserves provider-authored JSON numbers as `json.Number`,
including structured initial inputs, partial deltas, cancellation snapshots,
persistence, and replay. Callers asserting `float64` must migrate to `json.Number`
and explicitly choose `Int64`, `Float64`, or a lossless representation. Token
counters and cost fields retain their existing types. Signed empty assistant text
can be persisted; provider-specific replay still checks format and provenance.

Deferred Responses resolve auth once per HTTP attempt before building the route,
payload, and headers. Retries rebuild submissions and preserve the successful
attempt's conversion metadata and service tier. Submit, fetch, and cancel timeouts
cover auth, retry waits, headers, and response decoding. Handles contain no
credentials or auth-derived endpoints.

Chat Completions `n` and Google/Vertex `candidateCount` must be omitted or numeric
one, including after overrides and payload hooks. Nonzero alternatives cannot
alter the accumulated result. Radius premature EOF retains partial content and
returns transient retry advice; post-body retries remain caller-owned.

External embedding caches now require a non-secret `CacheNamespace`. Version 3
keys retain namespace and a deterministic configuration digest; implementations
must honor every field. Older entries, including version 2 entries, are bypassed
without scanning, rewriting, or deleting them. Callers must change
namespaces when opaque tenant, endpoint, custom transport, or provider identities
change. Warm cache paths perform the same preparation and validation as cold
paths and honor cancellation without authenticating. Batch option functions run
once. Retrieval insertions validate complete batches before publication and
preserve existing results and inferred dimensions on failure or cancellation.

`mise run go:build` is now a CGO-disabled compilation check of `./...`, including
examples and command packages. It emits no binary or root archive, uses no
`main.version` linker flags, and is included in CI. `mise run clean` remains
available explicitly. The embedding guide covers OpenAI-compatible, Gemini,
Vertex, and Bedrock adapters without changing release classifications.

## Compatibility

- Existing histories remain loadable and retain opaque metadata. Outbound
  reasoning replay now omits both modern and legacy metadata when any source
  provider/API/model component is absent or mismatched; no history migration is
  required, and ordinary text, calls, and results are preserved.
- Responses with cache writes can report higher costs because writes now use
  their checked-in cache-write rates. Provider token totals are unchanged.
- Valid composed-schema arguments previously rejected by implicit container
  typing may now succeed. Explicit types and object-only tool arguments remain
  enforced.

- `bedrock.ConverseUsage` adds `LongCacheWriteInputTokens`, the one-hour subset
  of total cache writes. Callers using unkeyed literals must update them; keyed
  literals can omit the field. Root usage/persistence schemas and catalog rates
  are unchanged, but one-hour cache cost estimates now apply the correct rate.
- Client-default `ToolChoiceNone` now suppresses model tool selection unless
  overridden. Incremental tool-event mutations no longer affect later events
  or terminal arguments; terminal `FinalMessage` and `Stream.Final` retain
  their existing shared-result contract.
- Bedrock completion without a supplied stop reason now returns an error,
  classified as transient with retry advice. Received output and usage are
  retained; no automatic replay after streamed output is introduced.
- Codex WebSocket connections bypass configured proxies for matching parent
  domains and exact IPv4/IPv6 exclusions, respecting optional ports. Leading-dot
  and `*.` forms continue matching roots as well as descendants. `*` also works
  inside exclusion lists; malformed entries and CIDR remain nonmatching.

- Explicit `base_url` or `baseURL` request configuration blocks auth-derived
  defaults under either spelling. Provider constructor and model-metadata header
  maps now follow the same deterministic case-insensitive merging rules as
  client/request options, with existing precedence and suppression retained.
- OpenAI image streams require `image_generation.completed` or
  `image_edit.completed`; EOF and `[DONE]` alone no longer indicate success.
  Explicit empty completions remain valid. Available output and usage survive
  incomplete-stream failures, and partial previews remain progress events.
  Missing image completion and Chat Completions `finish_reason` classify as
  transient/retryable without automatically replaying streamed requests.
- Canceled `InMemoryCredentialStore` modifications and deletions stop waiting for
  provider ownership promptly. Active callbacks retain serialized ownership and
  their existing commit behavior; they remain responsible for cooperative
  cancellation. Different providers can still progress independently.
- Truncated JSON credential redaction also accepts LF/CRLF and mixed JSON
  whitespace around field colons, preserving neighboring safe content and
  bounded UTF-8 previews.

- Codex WebSocket sessions reuse idle connections only when provider identity,
  effective URL, and final handshake headers match. Changed credentials or
  routing start fresh continuation state; overlapping requests and concurrent
  connection creation retain independent ownership and cleanup.
- Codex WebSocket, OpenAI images (including streaming, edits, and variations),
  and embeddings apply auth-derived routes, headers, and provider options before
  request construction. Explicit request configuration retains precedence and
  credentials resolve once per attempt. Header merges ignore capitalization,
  preserve the winning spelling, and resolve duplicate variants within one map
  in lexical order, with the last variant winning.
- An accepted text or image terminal result survives subsequent parent or
  collector cancellation and explicit closure. Blocked terminal delivery can be
  abandoned to close promptly; queued events remain readable and `Final`/`Err`
  retain the accepted outcome. Before acceptance, cancellation retains aborted
  partials. Closing OpenAI image streams or the root generation fallback cancels
  provider work, including stalled response reads, without synthesizing aborts.
- Anthropic Messages completion requires start and stop markers and a nonempty
  provider stop reason, even for empty output. Incomplete streams retain partials
  and classify as transient; automatic retry behavior is unchanged.
- Credential-field redaction covers unterminated JSON strings through the end of
  diagnostics, including escape boundaries and multiline values. Preview bounds
  and UTF-8 safety remain unchanged. Public interfaces, serialized formats,
  provider scope, and generated catalog metadata are unchanged by this hardening.

- `OpenAIResponsesCompat.SupportsMaxOutputTokens` uses the existing tri-state
  `OpenAICompatSupport` values. Unspecified or supported retains typed output
  limits and the minimum of 16; unsupported omits their automatic serialization.
  Explicit model sampling defaults, request sampling parameters, and raw body
  overrides remain available, with their existing precedence. Codex still
  removes the field after all overrides. Local validation and context budgeting
  are unchanged, but opting out means the provider does not enforce the typed
  output cap. No catalog row enables this opt-out by default.
- `SupportsExplicitPromptCacheMode` now selects the automatic long-cache wire
  format as well as explicit no-cache mode. Flagged models use
  `prompt_cache_options: {"ttl":"30m"}`; unflagged models retain
  `prompt_cache_retention: "24h"`, and unsupported long retention remains
  omitted. Typed `PromptCacheRetention` stays literal under its existing
  capability gate. Either cache field in request sampling or `extra_body`
  suppresses automatic long-cache directives, including explicit null values.
  Raw body values retain final precedence without nested-object merging or
  repair of caller-authored conflicts. Unset, short, none, affinity keys, and
  existing Azure/Codex catalog capabilities remain unchanged. Direct background
  submission uses the same builder and compatibility behavior.
- OpenCode retains one Zen provider and one Go provider because regional model
  availability is controlled by workspace opt-in rather than a distinct API
  endpoint or credential. Models requiring China hosting may return a
  `RegionError`; live probes now classify that result as upstream availability.
- Per-turn Anthropic thinking-effort replay is enabled only for direct Claude
  Opus 5. Fable 5, Sonnet 5, routed aliases, and caller-registered models remain
  unchanged unless their exact model configuration explicitly opts in.
- GitHub Copilot Claude Fable 5 now uses its Anthropic-compatible Messages
  route instead of Chat Completions. Selected reasoning levels are sent through
  adaptive thinking controls while authentication, Copilot headers, image and
  tool support, pricing, context limits, and other provider routes remain
  unchanged.
- The curated OpenRouter Claude Sonnet 5, DeepSeek V4 Pro, Gemini 3.5 Flash,
  GPT-5.2 Codex, and GPT-5.6 routes now expose only their reviewed reasoning
  efforts. Optional routes map omitted or explicitly disabled reasoning to
  nested effort `none`; mandatory routes omit a disabled-reasoning payload by
  default and reject explicit off locally. Pricing, limits, routing,
  authentication, provider registration, and the curated model set are
  unchanged.
- The Vertex Anthropic surface-probe route is diagnostic-only and remains
  outside `mise run ci`. It requires explicit project and location values plus
  a caller-supplied API key or OAuth access token; Sigma does not add ambient
  credential loading, persistence, model discovery, provider IDs, catalog
  rows, or runtime request changes. Existing default probe routes remain
  `zen,go`.
- The Google image surface-probe routes are diagnostic-only and remain outside
  `mise run ci`. Image mode still defaults to `openai`; the new routes add no
  ambient credential loading or persistence. Image generation remains a
  preview surface.
- Generated direct and Vertex Google image metadata replaces the retired
  `imagen-4.0-generate-001` rows with `gemini-3.1-flash-image`. The Vertex image
  adapter routes Gemini image models through `generateContent` and retains the
  existing Imagen `predict` behavior for caller-registered legacy model IDs;
  Sigma does not silently alias retired IDs. The catalog snapshot and generated
  artifacts now reflect the supported model set.
- Responses assistant phases remain opaque provider metadata rather than new
  provider-neutral content or end-turn controls. Unknown phases are retained
  for diagnostics but omitted from replay, as are recognized phases from a
  different provider, API, or model; partial and terminal stop-reason behavior
  is unchanged.
- OpenAI, Azure, and Codex Responses requests omit assistant turns whose
  persisted stop reason is `error` or `aborted`, together with tool results for
  calls from those turns. Successful, max-token, content-filter, and other
  non-failed history remains replayable, caller-owned messages and partial
  finals are unchanged, and Sigma does not automatically retry or replay the
  failed request.
- Xiaomi no longer advertises `mimo-v2-flash`, `mimo-v2-omni`, or
  `mimo-v2-pro` through its generated direct or regional Token Plan catalogs.
  Callers using those retired IDs must select a V2.5 model; Sigma does not
  silently alias or migrate model IDs. Xiaomi endpoints, credentials, request
  routing, and retained V2.5 metadata are unchanged.
- Omitting `WithToolChoice` preserves existing request payloads and Codex's
  automatic default. Existing typed and raw provider-specific tool controls
  override the provider-neutral fallback. `ToolChoiceNone` retains declared
  tool definitions on providers that support an explicit disabled choice;
  Bedrock instead omits active and replay-synthesized tool configuration because
  Converse has no disabled tool-choice variant. OpenAI-compatible Chat
  Completions preserve explicitly supplied provider-neutral and typed
  provider-specific choices even when no tool definitions are emitted,
  including fully deferred tool sets. Provider-specific typed choices override
  provider-neutral choices, and low-level payload overrides retain final
  precedence. Callers targeting endpoints that reject a choice without tools
  must omit the explicit option; Sigma no longer silently discards it.
- Direct xAI Grok 4.6 uses `/responses`, disables provider-side response
  storage, and requests encrypted reasoning whenever a supported effort is
  selected. Explicit `xhigh` maps directly to the provider effort; off and
  minimal remain unsupported. Long cache retention is omitted while cache keys
  and session affinity remain available. Grok 4.5 and the existing legacy Chat
  Completions routes are unchanged.
- Direct DeepSeek discovery removals leave caller-defined Chat Completions
  metadata supported. No Responses or Anthropic-compatible route, Files API
  image reference, live probe, or automatic price-window selection is added.
- OpenAI-compatible Chat Completions usage now falls back to top-level
  `cached_tokens` when nested cache details and `prompt_cache_hit_tokens` do not
  report a cache read. Cache reads remain included in provider prompt totals,
  so Sigma removes them from ordinary input tokens and prices them separately;
  raw usage, existing field precedence, and zero or omitted values are
  unchanged.
- Indexed OpenAI-compatible Chat Completions function and grammar tool calls
  now treat the first non-empty provider ID and name as authoritative. A
  provider ID arriving after Sigma generated a temporary ID replaces it once;
  later conflicting identity values are ignored while arguments, partial
  events, usage, raw finish reasons, and normalized stops remain unchanged.
  Indexless correlation behavior and request payloads are unchanged.
- OpenAI-compatible Chat Completions replays complete `reasoning_details` only
  when persisted provider, API, and model provenance are nonempty and exactly
  match the target, for both modern and legacy representations.
  Consecutive compatible text and summary fragments are coalesced into complete
  logical entries, with later fragments filling missing identity, format,
  index, and signature metadata without replacing prior values. Encrypted
  entries remain discrete and ordered. Conflicting nonempty IDs, formats, or
  signatures and differing supplied indexes (including zero) start new entries.
  Invalid or unknown entries are omitted
  individually, older tool-call metadata remains a same-provenance replay fallback, and
  requests without persisted details retain their previous payloads and
  defaults.
- Typed max-token options below 16 now serialize as 16 for
  OpenAI Responses-compatible and Azure OpenAI Responses requests. Unset values
  remain omitted, sampling parameters and raw `extra_body` values retain their
  existing precedence, and Codex Responses continues omitting
  `max_output_tokens`.
- Model-scoped arbitrary sampling defaults apply only to OpenAI-compatible Chat
  Completions, Responses, and Azure Responses. Omitted defaults leave existing
  payloads unchanged; Codex Responses and non-OpenAI APIs ignore the metadata,
  and broader provider-neutral sampling semantics remain deferred.
- Anthropic Messages refusal stops now retain non-empty `stop_details` in
  opaque assistant provider metadata alongside the raw `stop_reason`. Refusal
  and sensitive stops remain normalized as content filters, and null or empty
  details remain omitted.
- Blank or whitespace-only Google and Vertex assistant text or thinking with a
  missing, invalid, or cross-provider/API/model thought signature is no longer
  serialized as an empty part. Valid same-model signature-only parts remain
  replayable, and nonblank content remains present without an unusable
  signature. Caller-owned history and tool-call replay are unchanged.
- Non-terminal text events that previously omitted `PartialMessage` or left its
  stop reason empty now expose an empty or accumulated snapshot with
  `StopReasonPending`. Provider-supplied non-empty partial reasons and all
  successful, failed, or aborted terminal reasons remain unchanged. Consumers
  should persist only terminal messages; image streams and provider request
  behavior are unaffected.
- Anthropic-style OpenRouter Chat Completions cache markers now treat non-empty
  tool-result messages as eligible final conversation breakpoints. Empty tool
  results fall back to the preceding eligible message; disabled caching, other
  cache-control formats, tool-result fields, and the bounded system and final
  tool-definition markers remain unchanged.
- `kimi.DefaultUserAgent` now identifies requests as `sigma/kimi-coding` for
  both Kimi provider IDs. Provider, model, and request header overrides retain
  their existing precedence, and API-key and OAuth authentication are unchanged.
- Strict schema derivation does not add public tool types. Anthropic-compatible
  routes remain unchanged unless model metadata or the provider compatibility
  override enables strict tools; Bedrock and Google routes remain unchanged.
  Omitted or false strict metadata and unsupported routes preserve their
  existing payloads. Explicit strict schemas that use references, composed
  object or array unions, tuples, conditionals, pattern properties, or schema-
  valued additional properties fail before dispatch rather than relying on
  provider rejection; caller-owned schemas and tool-call arguments remain
  unchanged.
- Subscription metadata is informational only. It does not change OAuth login,
  credential selection, refresh timing, persistence, or provider dispatch, and
  custom OAuth descriptors remain generic unless callers opt in explicitly.
- GitHub Copilot model discovery is caller-invoked and advisory. It retries one
  rate-limited catalog request with a bounded provider delay and does not enable
  model policies. Explicit policy enablement retries up to two throttled POSTs
  within a five-second wait budget, honoring `Retry-After-Ms`, `Retry-After`,
  and context cancellation; other failures remain single-attempt. Neither
  helper runs during login or refresh, persists availability, mutates Sigma
  registries, or replaces generated catalog metadata. A valid empty account
  catalog produces a filter that matches no models.
- A superseded runtime model-source operation returns Sigma's existing conflict
  error and cannot replace the winning registry catalog, even when the newer
  operation fails. Source interfaces, source-owned network and cache side
  effects, automatic refresh behavior, persistence policy, and generated
  catalogs remain unchanged.
- OpenAI, Azure, and Codex Responses map incomplete `max_output_tokens` and
  `content_filter` reasons to successful normalized stops. Missing and unknown
  reasons return typed provider errors while preserving partial content, usage,
  cost, terminal status, Codex `end_turn`, and raw incomplete diagnostics.
- Upstream request-buffer exhaustion now produces a transient classification
  and same-model retry advice even when accompanied by a bad-request status.
  Sigma preserves partial finals and does not automatically replay post-body
  failures.
- Provider-wrapped DNS lookup failures, connection and socket/WebSocket
  closures, reset-before-headers and HTTP/2 no-response failures, explicit
  provider retry guidance, `ResourceExhausted` capacity failures, and known
  premature-stream diagnostics now produce transient classification and
  same-model retry advice when structured status or type evidence is
  unavailable. Auth, billing, quota, rate-limit, context-overflow, and
  cancellation precedence is unchanged; partial finals remain intact and
  post-body request replay remains caller-owned.
- Opt-in pre-body HTTP retries now include `408 Request Timeout` and
  `409 Conflict` responses alongside `429` and `5xx`. These statuses produce
  transient same-request retry advice when retries are disabled or exhausted,
  while structured provider codes and messages retain precedence. The default
  remains zero retries, and post-body replay remains caller-owned.
- Codex Responses accepts `response.done` as a successful terminal alias across
  SSE and WebSocket transports and retains an explicitly supplied `end_turn`
  boolean in assistant provider metadata. The value remains diagnostic and does
  not alter normalized stop reasons or agent control flow.
- OpenAI Responses and Codex Responses now retain function and custom-tool
  namespaces in opaque provider metadata through streaming and same-model
  replay. Cross-model replay keeps a namespace only when the matching deferred
  tool is loaded in the replayed request; incompatible providers and APIs omit
  it.
- Omitted or zero OAuth minimum-validity options retain existing provider
  refresh timing. Request requirements can lengthen but cannot shorten a
  provider's configured refresh window, and credentials without a known expiry
  remain usable without an early refresh.
- Google Generative AI and native Vertex Gemini 3 requests now preserve
  normalized tool-call IDs on replayed function calls and matching tool
  results. Older Vertex Gemini requests continue omitting unsupported IDs.
- Google Generative AI and Vertex responses containing function calls now map
  only an explicit `STOP` to `StopReasonToolCalls`. Max-token, provider-error,
  and unknown finish reasons retain their normalized stop reason, while the
  function call, usage, cost, terminal event, and raw `finishReason` remain
  available. Request payloads and defaults are unchanged.
- Amazon Bedrock Converse Stream service exceptions now retain the requested
  model and AWS request ID in typed provider errors and assistant diagnostics
  while preserving existing stop reasons and retry classification.
- Amazon Bedrock Converse Stream removes empty object-member names recursively
  only from outbound replayed tool inputs. Provider-emitted tool arguments,
  caller-owned messages, arrays, scalar values, `null`, and non-empty keys
  remain unchanged.
- Amazon Bedrock Converse Stream now decodes scalar base64 `redactedContent`
  deltas, joins their underlying bytes into one opaque redacted thinking block,
  and replays that blob before associated text or tool use. Invalid persisted
  blobs are omitted with their valid sibling content preserved, while malformed
  provider deltas remain typed provider failures with partial output intact.
- Qwen Token Plan now replaces the retired Qwen3.8 Max Preview ID with
  Qwen3.8 Max while preserving supported reasoning levels through native
  `reasoning_effort` controls on the international and China routes. Qwen3.7
  Max remains toggle-only. The Individual route preserves mapped reasoning
  efforts for DeepSeek V4, including Pro 0813, GLM-5.2, and Qwen3.8 Max while
  keeping Qwen3.6 Flash and both Qwen3.7 models toggle-only.
- Baseten GLM 5.2 accepts text and image input, including image-bearing tool
  results, while retaining its mapped off, high, and max reasoning efforts.
  Kimi K2.6 retains its existing image support and explicit thinking toggle
  without sending unsupported reasoning-effort values.
- Fireworks GLM 5.2 and GLM 5.2 Fast requests now send session affinity when
  prompt caching is enabled and omit unsupported explicit long-cache retention.
- Direct DeepSeek V4 Flash plus its OpenCode Zen and Go routes now
  map `ThinkingLevelLow` to the provider's `low` reasoning effort. DeepSeek V4
  Pro and models exposed through other routes retain their existing
  independently reviewed level mappings.
- Anthropic Messages streams now emit non-empty text and thinking delivered by
  content-block start events as ordered initial deltas while retaining
  signatures, citations, and complete final blocks.
- OpenAI-compatible Chat Completions streams configured without finish-reason
  support now infer normal or tool-call completion from assembled output after
  an explicit `[DONE]` marker. Default compatibility remains strict, and raw
  EOF without a terminal signal remains an error.

## Deferred work

- Codex WebSocket `NO_PROXY` suffix/IPv6 hardening, broader catalog additions and
  retirements, and additional routed Claude effort support remain deferred.
  Mistral lifecycle operations, broader cloud credential loading, and agent
  orchestration retain their existing boundaries. vLLM priority already fits
  the existing sampling-parameter surface.
- OpenCode Go may require workspace-level China-hosting opt-in for individual
  models. A later evidence-backed pass should identify the affected set, add
  explicit availability metadata and actionable diagnostics, and introduce a
  separate regional provider only if the endpoint or credential boundary
  actually differs.
- Deferred work continues to be tracked in [TODO.md](../TODO.md).

## Validation status

The GPT-6 Astra catalog addition passed catalog-backed local fixtures for both
Responses routes, including reasoning rejection before dispatch, image/function
tools, deferred-tool replay, cache controls, and cost boundaries with service
tiers. The catalog diff adds exactly two text rows without changing existing
rows. `mise run go:generate` refreshed the model outputs and snapshot headers;
`mise run go:fmt`, `mise run go:build`, `mise run ci` (including race tests),
and `git diff --check` passed. Validation made no live provider calls.

The Responses cache and output-token compatibility changes passed deterministic
payload and local-server coverage for direct, Azure, Codex, and background
requests, plus metadata persistence, registry copying, and generator tests.
`mise run go:generate` left the catalog and generated model outputs unchanged;
`mise run go:fmt`, `mise run ci` (including race tests), and `git diff --check`
passed. No live provider calls were made for this validation.

Validate this release with the process in [RELEASING.md](../RELEASING.md),
including the local CI-equivalent `mise run ci` gate before tagging.
