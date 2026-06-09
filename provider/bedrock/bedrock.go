// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package bedrock

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wintermi/sigma"
)

// Config carries Bedrock-specific settings without adding AWS SDK types to the
// root sigma package.
type Config struct {
	Region              string
	ModelID             string
	InferenceProfileARN string
	Endpoint            string
	CredentialSource    CredentialSource
}

// Provider adapts Amazon Bedrock Converse Stream to sigma.
//
// Known limitations:
//   - Image inputs support inline base64 bytes only. URL and S3 image shims are
//     intentionally not inferred from provider-neutral image blocks.
//   - Reasoning is sent through model-specific additional request fields when a
//     thinking token budget is provided; model-family support varies.
//   - Prompt caching is represented with Bedrock cache points, but cache TTL and
//     cacheable block placement differ across Anthropic, Nova, and other model
//     families.
//   - Bedrock model access, IAM permissions, and available model discovery are
//     AWS account concerns and are not handled by this adapter.
type Provider struct {
	config             Config
	client             ConverseStreamClient
	credentialDetector CredentialDetector
	clientFactory      converseClientFactory
}

type converseClientFactory func(context.Context, Config, sigma.Options, CredentialInfo) (ConverseStreamClient, error)

// ProviderOption configures a Provider.
type ProviderOption func(*Provider)

// NewProvider constructs a Bedrock Converse Stream provider.
func NewProvider(opts ...ProviderOption) *Provider {
	provider := &Provider{
		credentialDetector: DefaultCredentialDetector{},
		clientFactory:      newHTTPConverseStreamClient,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(provider)
		}
	}
	return provider
}

// WithConfig configures Bedrock request defaults.
func WithConfig(config Config) ProviderOption {
	return func(provider *Provider) {
		provider.config = config
	}
}

// WithRegion configures the AWS region used by the SDK client.
func WithRegion(region string) ProviderOption {
	return func(provider *Provider) {
		provider.config.Region = region
	}
}

// WithModelID configures the Bedrock modelId sent to ConverseStream.
func WithModelID(modelID string) ProviderOption {
	return func(provider *Provider) {
		provider.config.ModelID = modelID
	}
}

// WithInferenceProfileARN configures an inference profile ARN or ID as modelId.
func WithInferenceProfileARN(arn string) ProviderOption {
	return func(provider *Provider) {
		provider.config.InferenceProfileARN = arn
	}
}

// WithEndpoint configures a custom Bedrock Runtime endpoint.
func WithEndpoint(endpoint string) ProviderOption {
	return func(provider *Provider) {
		provider.config.Endpoint = strings.TrimRight(endpoint, "/")
	}
}

// WithCredentialSource configures which AWS credential path should be used.
func WithCredentialSource(source CredentialSource) ProviderOption {
	return func(provider *Provider) {
		provider.config.CredentialSource = source
	}
}

// WithConverseStreamClient injects a fakeable Bedrock client.
func WithConverseStreamClient(client ConverseStreamClient) ProviderOption {
	return func(provider *Provider) {
		provider.client = client
	}
}

// WithCredentialDetector injects credential detection for tests or custom AWS auth.
func WithCredentialDetector(detector CredentialDetector) ProviderOption {
	return func(provider *Provider) {
		if detector != nil {
			provider.credentialDetector = detector
		}
	}
}

// Register adds a Bedrock Converse Stream text provider to registry.
func Register(registry *sigma.Registry, providerID sigma.ProviderID, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(providerID, NewProvider(opts...))
}

// RegisterDefault adds a Bedrock Converse Stream provider to sigma's default registry.
func RegisterDefault(providerID sigma.ProviderID, opts ...ProviderOption) error {
	return sigma.RegisterDefaultTextProvider(providerID, NewProvider(opts...))
}

// API reports the Bedrock Converse Stream API surface.
func (p *Provider) API() sigma.API {
	return sigma.APIBedrockConverseStream
}

// Stream sends req to Bedrock ConverseStream and emits sigma events.
func (p *Provider) Stream(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) *sigma.Stream {
	ctx, cancel := sigma.ContextWithRequestTimeout(ctx, opts)
	stream, writer := sigma.NewStream(ctx)
	go func() {
		defer cancel()
		p.run(ctx, writer, model, req, opts)
	}()
	return stream
}

func (p *Provider) run(ctx context.Context, writer sigma.StreamWriter, model sigma.Model, req sigma.Request, opts sigma.Options) {
	final := sigma.AssistantMessage{
		Model:    model.ID,
		Provider: model.Provider,
	}

	effective := effectiveConfig(p.config, model, opts)
	payload, err := conversePayload(model, req, opts, effective)
	if err != nil {
		_ = writer.Error(ctx, err, final)
		return
	}
	if len(opts.TextPayloadDebugHooks) > 0 {
		body, err := json.Marshal(payload)
		if err != nil {
			_ = writer.Error(ctx, fmt.Errorf("bedrock converse stream: encode debug request: %w", err), final)
			return
		}
		if err := sigma.RunTextPayloadDebugHooks(ctx, opts, model.Provider, sigma.APIBedrockConverseStream, model.ID, body, nil); err != nil {
			_ = writer.Error(ctx, err, final)
			return
		}
	}

	credentials, err := p.credentialDetector.Detect(ctx, model, opts, effective)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			final.StopReason = sigma.StopReasonAborted
			_ = writer.Error(ctx, contextError(ctx, err), final)
			return
		}
		_ = writer.Error(ctx, err, final)
		return
	}

	client := p.client
	if client == nil {
		client, err = p.clientFactory(ctx, effective, opts, credentials)
		if err != nil {
			_ = writer.Error(ctx, err, final)
			return
		}
	}

	stream, err := client.ConverseStream(ctx, payload)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			final.StopReason = sigma.StopReasonAborted
			_ = writer.Error(ctx, contextError(ctx, err), final)
			return
		}
		final.StopReason = sigma.StopReasonError
		providerErr := providerError(model, err)
		final.Diagnostics = []sigma.Diagnostic{providerErr.Diagnostic()}
		_ = writer.Error(ctx, providerErr, final)
		return
	}
	defer func() {
		_ = stream.Close()
	}()

	responseFormat := opts.BedrockOptions != nil && opts.BedrockOptions.ResponseFormat != nil
	final, err = parseConverseStream(ctx, stream, writer, model, responseFormat)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			final.StopReason = sigma.StopReasonAborted
			_ = writer.Error(ctx, contextError(ctx, err), final)
			return
		}
		final.StopReason = sigma.StopReasonError
		var providerErr *sigma.ProviderError
		if errors.As(err, &providerErr) {
			final.Diagnostics = []sigma.Diagnostic{providerErr.Diagnostic()}
		}
		_ = writer.Error(ctx, err, final)
		return
	}
	_ = writer.Done(ctx, final)
}

func effectiveConfig(base Config, model sigma.Model, opts sigma.Options) Config {
	config := base
	options := providerOptions(opts, model.Provider)
	if value, ok := stringOption(options, providerOptionRegion); ok {
		config.Region = value
	}
	if value, ok := stringOption(options, providerOptionModelID); ok {
		config.ModelID = value
	} else if value, ok := stringOption(options, providerOptionModelIDGo); ok {
		config.ModelID = value
	}
	if value, ok := stringOption(options, providerOptionInferenceProfileARN); ok {
		config.InferenceProfileARN = value
	} else if value, ok := stringOption(options, providerOptionInferenceProfileARNGo); ok {
		config.InferenceProfileARN = value
	}
	if value, ok := stringOption(options, providerOptionEndpoint); ok {
		config.Endpoint = strings.TrimRight(value, "/")
	}
	if value, ok := stringOption(options, providerOptionCredentialSource); ok {
		config.CredentialSource = CredentialSource(value)
	} else if value, ok := stringOption(options, providerOptionCredentialSourceGo); ok {
		config.CredentialSource = CredentialSource(value)
	}
	if config.Region == "" {
		if region := inferenceProfileARNRegion(config.InferenceProfileARN); region != "" {
			config.Region = region
		} else if region := inferenceProfileARNRegion(string(model.ID)); region != "" {
			config.Region = region
		}
	}
	if config.Region == "" {
		config.Region = os.Getenv("AWS_REGION")
	}
	if config.Region == "" {
		config.Region = os.Getenv("AWS_DEFAULT_REGION")
	}
	if config.Region == "" && config.Endpoint == "" {
		applyModelEndpointFallback(&config, model)
	}
	return config
}

func applyModelEndpointFallback(config *Config, model sigma.Model) {
	baseURL := stringMetadata(model.ProviderMetadata, "baseURL")
	if region := regionalInferenceProfileRegion(model.ID); region != "" {
		config.Region = region
		if baseURL != "" {
			config.Endpoint = strings.TrimRight(strings.ReplaceAll(baseURL, "{region}", region), "/")
			return
		}
		config.Endpoint = "https://bedrock-runtime." + region + ".amazonaws.com"
		return
	}
	if baseURL == "" || strings.Contains(baseURL, "{region}") {
		return
	}
	config.Endpoint = strings.TrimRight(baseURL, "/")
	if region := bedrockEndpointRegion(config.Endpoint); region != "" {
		config.Region = region
	}
}

func regionalInferenceProfileRegion(modelID sigma.ModelID) string {
	if strings.HasPrefix(string(modelID), "eu.") {
		return "eu-central-1"
	}
	return ""
}

func inferenceProfileARNRegion(value string) string {
	parts := strings.Split(value, ":")
	if len(parts) < 6 {
		return ""
	}
	if parts[0] != "arn" || parts[2] != "bedrock" || parts[3] == "" {
		return ""
	}
	if parts[1] != "aws" && !strings.HasPrefix(parts[1], "aws-") {
		return ""
	}
	if !strings.HasPrefix(parts[5], "application-inference-profile/") {
		return ""
	}
	return parts[3]
}

func bedrockEndpointRegion(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parts := strings.Split(parsed.Hostname(), ".")
	if len(parts) < 4 || parts[0] != "bedrock-runtime" || parts[2] != "amazonaws" {
		return ""
	}
	return parts[1]
}

func newHTTPConverseStreamClient(_ context.Context, bedrockConfig Config, opts sigma.Options, credentialsInfo CredentialInfo) (ConverseStreamClient, error) {
	if bedrockConfig.Region == "" {
		return nil, &sigma.Error{
			Code:    sigma.ErrorUnsupported,
			Message: "bedrock converse stream: region is required",
		}
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return httpConverseClient{
		config:      bedrockConfig,
		credentials: credentialsInfo,
		httpClient:  httpClient,
		opts:        opts,
	}, nil
}

type httpConverseClient struct {
	config      Config
	credentials CredentialInfo
	httpClient  *http.Client
	opts        sigma.Options
}

func (c httpConverseClient) ConverseStream(ctx context.Context, req ConverseRequest) (ConverseStream, error) {
	body, err := awsConverseInput(req)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	endpoint := bedrockConverseStreamURL(c.config, req.ModelID)
	resp, err := sigma.DoHTTPWithRetry(
		ctx,
		c.httpClient,
		c.opts,
		func(ctx context.Context) (*http.Request, error) {
			httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
			if err != nil {
				return nil, err
			}
			httpReq.Header.Set("Content-Type", "application/json")
			httpReq.Header.Set("Accept", "application/vnd.amazon.eventstream")
			httpReq.Header.Set("User-Agent", "sigma/bedrock")
			for key, value := range c.opts.Headers {
				if !reservedBedrockHeader(key) {
					httpReq.Header.Set(key, value)
				}
			}
			if c.credentials.BearerToken != "" {
				httpReq.Header.Set("Authorization", "Bearer "+c.credentials.BearerToken)
			} else {
				signAWSSigV4(httpReq, data, c.credentials.AccessKeyID, c.credentials.SecretAccessKey, c.credentials.SessionToken, c.config.Region, "bedrock")
			}
			return httpReq, nil
		},
		func(resp *http.Response) *sigma.ProviderError {
			return sigma.NewProviderError(sigma.ProviderAmazonBedrock, sigma.APIBedrockConverseStream, sigma.ModelID(req.ModelID), resp.StatusCode, resp.Header.Get("x-amzn-requestid"), sigma.RetryAfter(resp.Header), nil, sigma.ErrProviderResponse)
		},
		sigma.TextResponseDebugHTTPHook(ctx, c.opts, sigma.ProviderAmazonBedrock, sigma.APIBedrockConverseStream, sigma.ModelID(req.ModelID)),
	)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, sigma.NewProviderError(sigma.ProviderAmazonBedrock, sigma.APIBedrockConverseStream, sigma.ModelID(req.ModelID), resp.StatusCode, resp.Header.Get("x-amzn-requestid"), sigma.RetryAfter(resp.Header), respBody, sigma.ErrProviderResponse)
	}
	return newHTTPConverseStream(resp.Body), nil
}

func reservedBedrockHeader(key string) bool {
	lower := strings.ToLower(key)
	return lower == "authorization" || lower == "host" || strings.HasPrefix(lower, "x-amz-")
}

func awsConverseInput(req ConverseRequest) (map[string]any, error) {
	input := make(map[string]any)
	if req.AdditionalModelRequestFields != nil {
		input["additionalModelRequestFields"] = req.AdditionalModelRequestFields
	}
	if len(req.System) > 0 {
		system, err := awsSystemBlocks(req.System)
		if err != nil {
			return nil, err
		}
		input["system"] = system
	}
	inferenceConfig, err := awsInferenceConfig(req.InferenceConfig)
	if err != nil {
		return nil, err
	}
	if inferenceConfig != nil {
		input["inferenceConfig"] = inferenceConfig
	}
	if len(req.Tools) > 0 {
		toolConfig := map[string]any{"tools": awsTools(req.Tools)}
		if choice := awsToolChoice(req.ToolChoice); choice != nil {
			toolConfig["toolChoice"] = choice
		}
		input["toolConfig"] = toolConfig
	}
	if len(req.AdditionalModelResponseFieldPaths) > 0 {
		input["additionalModelResponseFieldPaths"] = req.AdditionalModelResponseFieldPaths
	}
	if len(req.RequestMetadata) > 0 {
		input["requestMetadata"] = req.RequestMetadata
	}
	messages := make([]map[string]any, 0, len(req.Messages))
	for _, message := range req.Messages {
		content, err := awsContentBlocks(message.Content)
		if err != nil {
			return nil, err
		}
		messages = append(messages, map[string]any{
			"role":    awsRole(message.Role),
			"content": content,
		})
	}
	if len(messages) > 0 {
		input["messages"] = messages
	}
	return input, nil
}

func awsInferenceConfig(config *ConverseInferenceConfig) (map[string]any, error) {
	if config == nil {
		return nil, nil
	}
	input := make(map[string]any)
	if config.MaxTokens != nil {
		maxTokens, err := awsInt32(*config.MaxTokens, "max tokens")
		if err != nil {
			return nil, err
		}
		input["maxTokens"] = maxTokens
	}
	if config.Temperature != nil {
		input["temperature"] = *config.Temperature
	}
	if config.TopP != nil {
		input["topP"] = *config.TopP
	}
	if len(config.StopSequences) > 0 {
		input["stopSequences"] = append([]string(nil), config.StopSequences...)
	}
	return input, nil
}

func awsInt32(value int, name string) (int, error) {
	parsed, err := strconv.ParseInt(strconv.Itoa(value), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("bedrock converse stream: %s %d exceeds int32 range", name, value)
	}
	return int(parsed), nil
}

func awsSystemBlocks(blocks []ConverseContentBlock) ([]map[string]any, error) {
	converted := make([]map[string]any, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case converseBlockText:
			converted = append(converted, map[string]any{"text": block.Text})
		case converseBlockCachePoint:
			converted = append(converted, map[string]any{"cachePoint": awsCachePoint(block.CachePoint)})
		default:
			return nil, fmt.Errorf("bedrock converse stream: unsupported system block %q", block.Type)
		}
	}
	return converted, nil
}

func awsContentBlocks(blocks []ConverseContentBlock) ([]map[string]any, error) {
	converted := make([]map[string]any, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case converseBlockText:
			converted = append(converted, map[string]any{"text": block.Text})
		case converseBlockImage:
			image, err := awsImageBlock(block.Image)
			if err != nil {
				return nil, err
			}
			converted = append(converted, map[string]any{"image": image})
		case converseBlockToolUse:
			input := any(map[string]any{})
			if block.ToolUse.Input != nil {
				input = block.ToolUse.Input
			}
			converted = append(converted, map[string]any{"toolUse": map[string]any{
				"toolUseId": block.ToolUse.ID,
				"name":      block.ToolUse.Name,
				"input":     input,
			}})
		case converseBlockToolResult:
			converted = append(converted, map[string]any{"toolResult": awsToolResultBlock(block.ToolResult)})
		case converseBlockReasoning:
			converted = append(converted, awsReasoningBlock(block.Reasoning))
		case converseBlockCachePoint:
			converted = append(converted, map[string]any{"cachePoint": awsCachePoint(block.CachePoint)})
		default:
			return nil, fmt.Errorf("bedrock converse stream: unsupported content block %q", block.Type)
		}
	}
	return converted, nil
}

func awsImageBlock(image *ConverseImageBlock) (map[string]any, error) {
	if image == nil {
		return nil, fmt.Errorf("bedrock converse stream: image block is nil")
	}
	return map[string]any{
		"format": image.Format,
		"source": map[string]any{"bytes": image.Data},
	}, nil
}

func awsToolResultBlock(result *ConverseToolResultBlock) map[string]any {
	block := map[string]any{
		"toolUseId": result.ToolUseID,
		"content":   awsToolResultContent(result.Content),
	}
	if result.Status != "" {
		block["status"] = result.Status
	}
	return block
}

func awsToolResultContent(content []ConverseToolResultContent) []map[string]any {
	if len(content) == 0 {
		return []map[string]any{{"text": ""}}
	}
	converted := make([]map[string]any, 0, len(content))
	for _, item := range content {
		switch item.Type {
		case converseBlockImage:
			image, err := awsImageBlock(item.Image)
			if err == nil {
				converted = append(converted, map[string]any{"image": image})
			}
		default:
			converted = append(converted, map[string]any{"text": item.Text})
		}
	}
	return converted
}

func awsCachePoint(cachePoint *ConverseCachePointBlock) map[string]any {
	out := map[string]any{"type": "default"}
	if cachePoint != nil && cachePoint.TTL != "" {
		out["ttl"] = cachePoint.TTL
	}
	return out
}

func awsReasoningBlock(reasoning *ConverseReasoningBlock) map[string]any {
	if reasoning == nil {
		return map[string]any{"reasoningContent": map[string]any{}}
	}
	if reasoning.Redacted {
		return map[string]any{"reasoningContent": map[string]any{
			"redactedReasoning": map[string]any{"data": reasoning.ProviderSignature},
		}}
	}
	return map[string]any{"reasoningContent": map[string]any{
		"reasoningText": map[string]any{
			"text":      reasoning.Text,
			"signature": reasoning.Signature,
		},
	}}
}

func awsTools(tools []ConverseTool) []map[string]any {
	converted := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		spec := map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
			"inputSchema": map[string]any{"json": tool.InputSchema},
		}
		converted = append(converted, map[string]any{"toolSpec": spec})
	}
	return converted
}

func awsToolChoice(choice *sigma.BedrockToolChoice) map[string]any {
	if choice == nil {
		return nil
	}
	switch choice.Type {
	case sigma.BedrockToolChoiceAuto:
		return map[string]any{"auto": map[string]any{}}
	case sigma.BedrockToolChoiceAny:
		return map[string]any{"any": map[string]any{}}
	case sigma.BedrockToolChoiceTool:
		if choice.Name == "" {
			return nil
		}
		return map[string]any{"tool": map[string]any{"name": choice.Name}}
	default:
		return nil
	}
}

func awsRole(role string) string {
	switch role {
	case "assistant":
		return "assistant"
	default:
		return "user"
	}
}

type httpConverseStream struct {
	body   io.ReadCloser
	events chan ConverseEvent
	err    error
	once   sync.Once
}

func newHTTPConverseStream(body io.ReadCloser) *httpConverseStream {
	stream := &httpConverseStream{
		body:   body,
		events: make(chan ConverseEvent),
	}
	go stream.forward()
	return stream
}

func (s *httpConverseStream) Events() <-chan ConverseEvent {
	return s.events
}

func (s *httpConverseStream) Close() error {
	var err error
	s.once.Do(func() {
		if s.body != nil {
			if closeErr := s.body.Close(); closeErr != nil {
				err = fmt.Errorf("bedrock converse stream: close response body: %w", closeErr)
			}
		}
	})
	return err
}

func (s *httpConverseStream) Err() error {
	return s.err
}

func (s *httpConverseStream) forward() {
	defer close(s.events)
	defer func() { _ = s.Close() }()
	decoder := newEventStreamDecoder(s.body)
	for {
		frame, err := decoder.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.err = err
			}
			return
		}
		event, ok := converseEventFromFrame(frame)
		if !ok {
			continue
		}
		s.events <- event
	}
}

func converseEventFromFrame(frame *eventStreamFrame) (ConverseEvent, bool) {
	if frame.MessageType == "exception" {
		return ConverseEvent{
			Kind: ConverseEventError,
			Err:  fmt.Errorf("bedrock converse stream: %s: %s", frame.EventType, string(frame.Payload)),
		}, true
	}
	if frame.MessageType != "event" {
		return ConverseEvent{}, false
	}
	var payload map[string]any
	if len(frame.Payload) > 0 {
		if err := json.Unmarshal(frame.Payload, &payload); err != nil {
			return ConverseEvent{Kind: ConverseEventError, Err: err}, true
		}
	}
	switch frame.EventType {
	case "messageStart":
		return ConverseEvent{Kind: ConverseEventMessageStart, Role: stringValue(payload, "role")}, true
	case "contentBlockStart":
		converted := ConverseEvent{
			Kind:              ConverseEventContentBlockStart,
			ContentBlockIndex: intValue(payload, "contentBlockIndex"),
		}
		if start, ok := payload["start"].(map[string]any); ok {
			if tool, ok := start["toolUse"].(map[string]any); ok {
				converted.ToolUseID = stringValue(tool, "toolUseId")
				converted.ToolName = stringValue(tool, "name")
			}
		}
		return converted, true
	case "contentBlockDelta":
		return deltaEvent(payload), true
	case "contentBlockStop":
		return ConverseEvent{Kind: ConverseEventContentBlockStop, ContentBlockIndex: intValue(payload, "contentBlockIndex")}, true
	case "messageStop":
		return ConverseEvent{Kind: ConverseEventMessageStop, StopReason: stringValue(payload, "stopReason")}, true
	case "metadata":
		return ConverseEvent{Kind: ConverseEventMetadata, Usage: usageFromPayload(payload)}, true
	default:
		return ConverseEvent{}, false
	}
}

func deltaEvent(payload map[string]any) ConverseEvent {
	converted := ConverseEvent{
		Kind:              ConverseEventContentBlockDelta,
		ContentBlockIndex: intValue(payload, "contentBlockIndex"),
	}
	delta, _ := payload["delta"].(map[string]any)
	if text, ok := delta["text"].(string); ok {
		converted.TextDelta = text
	}
	if tool, ok := delta["toolUse"].(map[string]any); ok {
		converted.ToolInputDelta = stringValue(tool, "input")
	}
	if reasoning, ok := delta["reasoningContent"].(map[string]any); ok {
		if text, ok := reasoning["text"].(string); ok {
			converted.ThinkingDelta = text
		}
		if signature, ok := reasoning["signature"].(string); ok {
			converted.ThinkingSignature = signature
		}
		if wrapped, ok := reasoning["reasoningText"].(map[string]any); ok {
			converted.ThinkingDelta = stringValue(wrapped, "text")
			converted.ThinkingSignature = stringValue(wrapped, "signature")
		}
		if redacted, ok := reasoning["redactedReasoning"].(map[string]any); ok {
			converted.RedactedThinking = stringValue(redacted, "data")
		}
	}
	return converted
}

func usageFromPayload(payload map[string]any) *ConverseUsage {
	usage, _ := payload["usage"].(map[string]any)
	if len(usage) == 0 {
		return nil
	}
	return &ConverseUsage{
		InputTokens:           intValue(usage, "inputTokens"),
		OutputTokens:          intValue(usage, "outputTokens"),
		TotalTokens:           intValue(usage, "totalTokens"),
		CacheReadInputTokens:  intValue(usage, "cacheReadInputTokens"),
		CacheWriteInputTokens: intValue(usage, "cacheWriteInputTokens"),
	}
}

func bedrockConverseStreamURL(config Config, modelID string) string {
	base := strings.TrimRight(config.Endpoint, "/")
	if base == "" {
		base = fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", config.Region)
	}
	escaped := url.PathEscape(modelID)
	return base + "/model/" + escaped + "/converse-stream"
}

func signAWSSigV4(req *http.Request, body []byte, accessKey string, secretKey string, sessionToken string, region string, service string) {
	now := time.Now().UTC()
	dateStamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")

	req.Header.Set("x-amz-date", amzDate)
	if sessionToken != "" {
		req.Header.Set("x-amz-security-token", sessionToken)
	}
	payloadHash := sha256Hex(body)
	req.Header.Set("x-amz-content-sha256", payloadHash)

	signedHeaders := []string{"content-type", "host"}
	for header := range req.Header {
		lower := strings.ToLower(header)
		if strings.HasPrefix(lower, "x-amz-") {
			signedHeaders = append(signedHeaders, lower)
		}
	}
	sort.Strings(signedHeaders)

	var canonicalHeaders strings.Builder
	for _, header := range signedHeaders {
		value := req.Header.Get(header)
		if header == "host" {
			value = req.URL.Host
		}
		canonicalHeaders.WriteString(header)
		canonicalHeaders.WriteString(":")
		canonicalHeaders.WriteString(strings.TrimSpace(value))
		canonicalHeaders.WriteString("\n")
	}

	canonicalRequest := strings.Join([]string{
		req.Method,
		uriEncodePath(req.URL.Path),
		req.URL.RawQuery,
		canonicalHeaders.String(),
		strings.Join(signedHeaders, ";"),
		payloadHash,
	}, "\n")

	credentialScope := dateStamp + "/" + region + "/" + service + "/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + credentialScope + "\n" + sha256Hex([]byte(canonicalRequest))
	signingKey := hmacSHA256(hmacSHA256(hmacSHA256(hmacSHA256(
		[]byte("AWS4"+secretKey), []byte(dateStamp)),
		[]byte(region)),
		[]byte(service)),
		[]byte("aws4_request"))
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))
	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		accessKey,
		credentialScope,
		strings.Join(signedHeaders, ";"),
		signature,
	))
}

func uriEncodePath(path string) string {
	var encoded strings.Builder
	for i := 0; i < len(path); i++ {
		c := path[i]
		if c == '/' || isUnreserved(c) {
			encoded.WriteByte(c)
			continue
		}
		_, _ = fmt.Fprintf(&encoded, "%%%02X", c)
	}
	return encoded.String()
}

func isUnreserved(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~'
}

func sha256Hex(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func hmacSHA256(key []byte, data []byte) []byte {
	hash := hmac.New(sha256.New, key)
	_, _ = hash.Write(data)
	return hash.Sum(nil)
}

func stringValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func intValue(values map[string]any, key string) int {
	switch value := values[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return 0
	}
}

func providerError(model sigma.Model, err error) *sigma.ProviderError {
	var providerErr *sigma.ProviderError
	if errors.As(err, &providerErr) {
		return providerErr
	}
	statusCode, requestID := providerErrorMetadata(err)
	return sigma.NewProviderError(
		model.Provider,
		sigma.APIBedrockConverseStream,
		model.ID,
		statusCode,
		requestID,
		0,
		[]byte(err.Error()),
		err,
	)
}

func contextError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return &sigma.Error{Code: sigma.ErrorAborted, Message: ctxErr.Error(), Err: ctxErr}
	}
	return &sigma.Error{Code: sigma.ErrorAborted, Message: err.Error(), Err: err}
}
