// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

// Package radius adapts the Radius gateway messages API to Sigma.
package radius

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/sse"
	"github.com/wintermi/sigma/internal/streamlifecycle"
)

const (
	// DefaultGatewayURL is the default Radius gateway URL.
	DefaultGatewayURL = "https://radius.pi.dev"

	radiusModelBaseURL  = "radiusBaseURL"
	maxCatalogBodyBytes = 1 << 20
)

// Provider adapts the Radius messages API to Sigma's text-provider interface.
type Provider struct {
	config *config
}

// ModelSource discovers Radius models from the configured gateway.
type ModelSource struct {
	config *config
}

type config struct {
	gatewayURL    string
	httpClient    *http.Client
	headers       map[string]string
	catalogAPIKey string
}

// ProviderOption configures a Radius provider and its model source.
type ProviderOption func(*config)

// NewProvider constructs a Radius messages provider.
func NewProvider(opts ...ProviderOption) *Provider {
	return &Provider{config: newConfig(opts...)}
}

// NewModelSource constructs a runtime source for Radius gateway models.
func NewModelSource(opts ...ProviderOption) *ModelSource {
	return &ModelSource{config: newConfig(opts...)}
}

// WithGatewayURL configures the Radius gateway URL, for example an httptest server URL.
func WithGatewayURL(gatewayURL string) ProviderOption {
	return func(config *config) {
		config.gatewayURL = strings.TrimRight(gatewayURL, "/")
	}
}

// WithHTTPClient configures the HTTP client used for catalog and message requests.
func WithHTTPClient(client *http.Client) ProviderOption {
	return func(config *config) {
		config.httpClient = client
	}
}

// WithHeader configures a provider default request header.
func WithHeader(key, value string) ProviderOption {
	return WithHeaders(map[string]string{key: value})
}

// WithHeaders configures provider default request headers.
func WithHeaders(headers map[string]string) ProviderOption {
	return func(config *config) {
		if len(headers) == 0 {
			return
		}
		if config.headers == nil {
			config.headers = make(map[string]string, len(headers))
		}
		for key, value := range headers {
			config.headers[key] = value
		}
	}
}

// WithCatalogAPIKey configures the API key used exclusively to refresh the
// gateway catalog. When omitted, refresh uses RADIUS_API_KEY.
func WithCatalogAPIKey(apiKey string) ProviderOption {
	return func(config *config) {
		config.catalogAPIKey = apiKey
	}
}

// Register adds the Radius text provider and runtime model source to registry.
func Register(registry *sigma.Registry, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	config := newConfig(opts...)
	if err := registry.RegisterTextProvider(sigma.ProviderRadius, &Provider{config: config}); err != nil {
		return fmt.Errorf("radius: register text provider: %w", err)
	}
	if err := registry.RegisterTextModelSource(sigma.ProviderRadius, &ModelSource{config: config}); err != nil {
		return fmt.Errorf("radius: register model source: %w", err)
	}
	return nil
}

// RegisterDefault adds the Radius text provider and runtime model source to
// Sigma's default registry.
func RegisterDefault(opts ...ProviderOption) error {
	config := newConfig(opts...)
	if err := sigma.RegisterDefaultTextProvider(sigma.ProviderRadius, &Provider{config: config}); err != nil {
		return fmt.Errorf("radius: register default text provider: %w", err)
	}
	if err := sigma.RegisterDefaultTextModelSource(sigma.ProviderRadius, &ModelSource{config: config}); err != nil {
		return fmt.Errorf("radius: register default model source: %w", err)
	}
	return nil
}

// API reports the Radius messages API surface.
func (*Provider) API() sigma.API {
	return sigma.APIRadiusMessages
}

// TextModels fetches and validates the current model catalog from Radius.
func (source *ModelSource) TextModels(ctx context.Context) ([]sigma.Model, error) {
	if source == nil || source.config == nil {
		return nil, errors.New("radius: model source is required")
	}
	endpoint, err := endpoint(source.config.gatewayURL, "/v1/config")
	if err != nil {
		return nil, fmt.Errorf("radius: catalog endpoint: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("radius: create catalog request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if apiKey := source.config.catalogKey(); apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	for key, value := range source.config.headers {
		req.Header.Set(key, value)
	}

	resp, err := source.config.httpClientOrDefault().Do(req)
	if err != nil {
		return nil, fmt.Errorf("radius: fetch catalog: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("radius: read catalog: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, sigma.NewProviderError(
			sigma.ProviderRadius,
			sigma.APIRadiusMessages,
			"",
			resp.StatusCode,
			requestID(resp.Header),
			sigma.RetryAfter(resp.Header),
			body,
			nil,
		)
	}

	var catalog gatewayConfig
	if err := json.Unmarshal(body, &catalog); err != nil {
		return nil, fmt.Errorf("radius: decode catalog: %w", err)
	}
	baseURL, err := validBaseURL(catalog.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("radius: invalid catalog: %w", err)
	}

	models := make([]sigma.Model, 0, len(catalog.Models))
	seen := make(map[sigma.ModelID]struct{}, len(catalog.Models))
	for _, entry := range catalog.Models {
		model, ok := entry.model(baseURL)
		if !ok {
			continue
		}
		if _, exists := seen[model.ID]; exists {
			continue
		}
		seen[model.ID] = struct{}{}
		models = append(models, model)
	}
	return models, nil
}

// Stream sends a Radius messages request and emits Sigma events as SSE chunks arrive.
func (provider *Provider) Stream(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) *sigma.Stream {
	ctx, stream, writer, cleanup := streamlifecycle.NewTextStream(ctx, opts)
	go func() {
		defer cleanup()
		provider.run(ctx, writer, model, req, opts)
	}()
	return stream
}

func (provider *Provider) run(ctx context.Context, writer sigma.StreamWriter, model sigma.Model, req sigma.Request, opts sigma.Options) {
	final := sigma.AssistantMessage{Model: model.ID, Provider: model.Provider}
	resp, err := provider.do(ctx, model, req, opts)
	if err != nil {
		if isContextError(ctx, err) {
			final.StopReason = sigma.StopReasonAborted
			_ = writer.Error(ctx, abortedError(ctx, err), final)
			return
		}
		_ = writer.Error(ctx, err, final)
		return
	}
	defer resp.Body.Close()
	body := sse.CloseOnContextDone(ctx, resp.Body)
	defer body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		providerErr := responseError(resp, model)
		final.StopReason = sigma.StopReasonError
		final.Diagnostics = []sigma.Diagnostic{providerErr.Diagnostic()}
		_ = writer.Error(ctx, providerErr, final)
		return
	}

	final, err = parseStream(ctx, body, writer, model)
	if err != nil {
		if isContextError(ctx, err) {
			final.StopReason = sigma.StopReasonAborted
			_ = writer.Error(ctx, abortedError(ctx, err), final)
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

func (provider *Provider) do(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) (*http.Response, error) {
	return sigma.DoHTTPWithRetry(
		ctx,
		provider.config.requestHTTPClient(opts),
		opts,
		func(ctx context.Context) (*http.Request, error) {
			return provider.newRequest(ctx, model, req, opts)
		},
		func(resp *http.Response) *sigma.ProviderError {
			return responseError(resp, model)
		},
		sigma.TextResponseDebugHTTPHook(ctx, opts, model.Provider, sigma.APIRadiusMessages, model.ID),
	)
}

func (provider *Provider) newRequest(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) (*http.Request, error) {
	payload, err := requestPayload(model, req, opts)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("radius messages: encode request: %w", err)
	}
	baseURL := provider.config.gatewayURL
	if model.ProviderMetadata != nil {
		if configured, ok := model.ProviderMetadata[radiusModelBaseURL].(string); ok && configured != "" {
			baseURL = configured
		}
	}
	endpoint, err := endpoint(baseURL, "/messages")
	if err != nil {
		return nil, fmt.Errorf("radius messages: endpoint: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("radius messages: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("User-Agent", "sigma/radius-messages")
	if err := addAuthHeader(ctx, httpReq, model, opts); err != nil {
		return nil, err
	}
	for key, value := range provider.config.headers {
		httpReq.Header.Set(key, value)
	}
	for key, value := range opts.Headers {
		httpReq.Header.Set(key, value)
	}
	sigma.ApplySuppressedHeaders(httpReq.Header, opts)
	if err := sigma.RunTextPayloadDebugHooks(ctx, opts, model.Provider, sigma.APIRadiusMessages, model.ID, body, httpReq.Header); err != nil {
		return nil, err
	}
	return httpReq, nil
}

func newConfig(opts ...ProviderOption) *config {
	config := &config{gatewayURL: DefaultGatewayURL}
	for _, opt := range opts {
		if opt != nil {
			opt(config)
		}
	}
	return config
}

func (config *config) httpClientOrDefault() *http.Client {
	if config != nil && config.httpClient != nil {
		return config.httpClient
	}
	return http.DefaultClient
}

func (config *config) requestHTTPClient(opts sigma.Options) *http.Client {
	if opts.HTTPClient != nil {
		return opts.HTTPClient
	}
	return config.httpClientOrDefault()
}

func (config *config) catalogKey() string {
	if config != nil && config.catalogAPIKey != "" {
		return config.catalogAPIKey
	}
	return os.Getenv("RADIUS_API_KEY")
}

func endpoint(baseURL, suffix string) (string, error) {
	baseURL, err := validBaseURL(baseURL)
	if err != nil {
		return "", err
	}
	return baseURL + suffix, nil
}

func validBaseURL(value string) (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(value), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("invalid base URL %q", value)
	}
	return baseURL, nil
}

func addAuthHeader(ctx context.Context, req *http.Request, model sigma.Model, opts sigma.Options) error {
	if opts.AuthResolver == nil {
		return &sigma.Error{
			Code:     sigma.ErrorUnsupported,
			Message:  "radius messages: auth resolver is required",
			Provider: model.Provider,
			Model:    model.ID,
		}
	}
	credential, err := opts.AuthResolver.Resolve(ctx, model, opts)
	if err != nil {
		return err
	}
	if credential.Value != "" {
		req.Header.Set("Authorization", "Bearer "+credential.Value)
	}
	return nil
}

type gatewayConfig struct {
	BaseURL string         `json:"baseUrl"`
	Models  []gatewayModel `json:"models"`
}

type gatewayModel struct {
	ID               string                         `json:"id"`
	Name             string                         `json:"name"`
	Reasoning        bool                           `json:"reasoning"`
	ThinkingLevelMap map[sigma.ThinkingLevel]string `json:"thinkingLevelMap"`
	Input            []string                       `json:"input"`
	Cost             gatewayCost                    `json:"cost"`
	ContextWindow    int                            `json:"contextWindow"`
	MaxTokens        int                            `json:"maxTokens"`
}

type gatewayCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

func (entry gatewayModel) model(baseURL string) (sigma.Model, bool) {
	if entry.ID == "" || entry.Name == "" || entry.ContextWindow <= 0 || entry.MaxTokens < 0 || entry.Cost.Input < 0 || entry.Cost.Output < 0 || entry.Cost.CacheRead < 0 || entry.Cost.CacheWrite < 0 {
		return sigma.Model{}, false
	}
	inputs := make([]sigma.ContentBlockType, 0, len(entry.Input))
	for _, input := range entry.Input {
		switch input {
		case string(sigma.ContentBlockText):
			inputs = append(inputs, sigma.ContentBlockText)
		case string(sigma.ContentBlockImage):
			inputs = append(inputs, sigma.ContentBlockImage)
		}
	}
	if !containsText(inputs) {
		return sigma.Model{}, false
	}
	thinkingMap := make(map[sigma.ThinkingLevel]string, len(entry.ThinkingLevelMap))
	thinkingLevels := make([]sigma.ThinkingLevel, 0, len(entry.ThinkingLevelMap))
	for level, value := range entry.ThinkingLevelMap {
		if level == "" || level == sigma.ThinkingLevelOff || value == "" {
			continue
		}
		thinkingMap[level] = value
		thinkingLevels = append(thinkingLevels, level)
	}
	sort.Slice(thinkingLevels, func(i, j int) bool { return thinkingLevels[i] < thinkingLevels[j] })
	if len(thinkingMap) == 0 {
		thinkingMap = nil
	}
	return sigma.Model{
		ID:                            sigma.ModelID(entry.ID),
		Provider:                      sigma.ProviderRadius,
		API:                           sigma.APIRadiusMessages,
		Name:                          entry.Name,
		ContextWindow:                 entry.ContextWindow,
		MaxOutputTokens:               entry.MaxTokens,
		SupportedInputs:               inputs,
		SupportsTools:                 true,
		SupportsThinking:              entry.Reasoning,
		ThinkingLevels:                thinkingLevels,
		ThinkingLevelMap:              thinkingMap,
		InputCostPerMillion:           entry.Cost.Input,
		OutputCostPerMillion:          entry.Cost.Output,
		CacheReadInputCostPerMillion:  entry.Cost.CacheRead,
		CacheWriteInputCostPerMillion: entry.Cost.CacheWrite,
		CostCurrency:                  "USD",
		DefaultTransport:              sigma.TransportSSE,
		ProviderMetadata: map[string]any{
			radiusModelBaseURL:          baseURL,
			sigma.MetadataAPIKeyEnvVars: []string{"RADIUS_API_KEY"},
		},
	}, true
}

func containsText(inputs []sigma.ContentBlockType) bool {
	for _, input := range inputs {
		if input == sigma.ContentBlockText {
			return true
		}
	}
	return false
}

type radiusPayload struct {
	Model   string        `json:"model"`
	Context radiusContext `json:"context"`
	Options radiusOptions `json:"options"`
}

type radiusContext struct {
	SystemPrompt string          `json:"systemPrompt,omitempty"`
	Messages     []radiusMessage `json:"messages"`
	Tools        []radiusTool    `json:"tools,omitempty"`
}

type radiusOptions struct {
	Temperature    *float64 `json:"temperature,omitempty"`
	MaxTokens      *int     `json:"maxTokens,omitempty"`
	Reasoning      string   `json:"reasoning,omitempty"`
	CacheRetention string   `json:"cacheRetention,omitempty"`
	SessionID      string   `json:"sessionId,omitempty"`
}

type radiusMessage struct {
	Role           string       `json:"role"`
	Content        any          `json:"content,omitempty"`
	ToolCallID     string       `json:"toolCallId,omitempty"`
	ToolName       string       `json:"toolName,omitempty"`
	AddedToolNames []string     `json:"addedToolNames,omitempty"`
	IsError        bool         `json:"isError,omitempty"`
	Provider       string       `json:"provider,omitempty"`
	API            string       `json:"api,omitempty"`
	Model          string       `json:"model,omitempty"`
	StopReason     string       `json:"stopReason,omitempty"`
	Usage          *sigma.Usage `json:"usage,omitempty"`
}

type radiusTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

func requestPayload(model sigma.Model, req sigma.Request, opts sigma.Options) (radiusPayload, error) {
	messages := make([]radiusMessage, 0, len(req.Messages))
	for _, message := range req.Messages {
		converted, err := messagePayload(message)
		if err != nil {
			return radiusPayload{}, err
		}
		messages = append(messages, converted)
	}
	tools := make([]radiusTool, 0, len(req.Tools))
	for _, tool := range req.Tools {
		if tool.ProviderDefinedType != "" {
			return radiusPayload{}, unsupportedError(model, "provider-defined tools are not supported")
		}
		tools = append(tools, radiusTool{Name: tool.Name, Description: tool.Description, Parameters: tool.InputSchema})
	}
	options := radiusOptions{
		Temperature:    opts.Temperature,
		MaxTokens:      opts.MaxTokens,
		CacheRetention: string(opts.CacheRetention),
		SessionID:      opts.SessionID,
	}
	if opts.ReasoningLevel != "" {
		if opts.ReasoningLevel == sigma.ThinkingLevelOff {
			options.Reasoning = string(sigma.ThinkingLevelOff)
		} else if value, ok := model.ProviderThinkingLevel(opts.ReasoningLevel); ok {
			options.Reasoning = value
		} else {
			return radiusPayload{}, unsupportedError(model, fmt.Sprintf("thinking level %q is not supported", opts.ReasoningLevel))
		}
	}
	return radiusPayload{
		Model: string(model.ID),
		Context: radiusContext{
			SystemPrompt: req.SystemPrompt,
			Messages:     messages,
			Tools:        tools,
		},
		Options: options,
	}, nil
}

func messagePayload(message sigma.Message) (radiusMessage, error) {
	content, err := contentPayload(message.Content)
	if err != nil {
		return radiusMessage{}, err
	}
	switch message.Role {
	case sigma.RoleUser, sigma.RoleDeveloper:
		return radiusMessage{Role: "user", Content: content}, nil
	case sigma.RoleAssistant:
		return radiusMessage{
			Role:       "assistant",
			Content:    content,
			Provider:   string(message.Provider),
			API:        string(message.API),
			Model:      string(message.Model),
			StopReason: string(message.StopReason),
			Usage:      message.Usage,
		}, nil
	case sigma.RoleTool:
		return radiusMessage{
			Role:           "toolResult",
			Content:        content,
			ToolCallID:     message.ToolCallID,
			ToolName:       message.ToolName,
			AddedToolNames: append([]string(nil), message.AddedToolNames...),
			IsError:        message.IsError,
		}, nil
	default:
		return radiusMessage{}, &sigma.Error{Code: sigma.ErrorUnsupported, Message: fmt.Sprintf("radius messages: unsupported role %q", message.Role)}
	}
}

func contentPayload(blocks []sigma.ContentBlock) ([]map[string]any, error) {
	content := make([]map[string]any, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case sigma.ContentBlockText:
			content = append(content, map[string]any{"type": "text", "text": block.Text})
		case sigma.ContentBlockThinking:
			entry := map[string]any{"type": "thinking", "thinking": block.ThinkingText}
			if block.Signature != "" {
				entry["thinkingSignature"] = block.Signature
			}
			if block.Redacted {
				entry["redacted"] = true
			}
			content = append(content, entry)
		case sigma.ContentBlockImage:
			if block.ImageSource != "" && block.ImageSource != "base64" {
				return nil, &sigma.Error{Code: sigma.ErrorUnsupported, Message: "radius messages: only base64 image content is supported"}
			}
			if block.Data == "" || block.MIMEType == "" {
				return nil, &sigma.Error{Code: sigma.ErrorUnsupported, Message: "radius messages: image data and MIME type are required"}
			}
			content = append(content, map[string]any{"type": "image", "data": block.Data, "mimeType": block.MIMEType})
		case sigma.ContentBlockToolCall:
			content = append(content, map[string]any{
				"type":      "toolCall",
				"id":        block.ToolCallID,
				"name":      block.ToolName,
				"arguments": block.ToolArguments,
			})
		case sigma.ContentBlockDocument:
			return nil, &sigma.Error{Code: sigma.ErrorUnsupported, Message: "radius messages: document content is not supported"}
		default:
			return nil, &sigma.Error{Code: sigma.ErrorUnsupported, Message: fmt.Sprintf("radius messages: unsupported content block %q", block.Type)}
		}
	}
	return content, nil
}

func unsupportedError(model sigma.Model, message string) error {
	return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "radius messages: " + message, Provider: model.Provider, Model: model.ID}
}

type radiusEvent struct {
	Type             string          `json:"type"`
	ContentIndex     int             `json:"contentIndex"`
	Delta            string          `json:"delta"`
	Content          string          `json:"content"`
	ContentSignature string          `json:"contentSignature"`
	Redacted         bool            `json:"redacted"`
	ID               string          `json:"id"`
	ToolName         string          `json:"toolName"`
	ToolCall         *radiusToolCall `json:"toolCall"`
	Reason           string          `json:"reason"`
	Usage            *radiusUsage    `json:"usage"`
	ResponseID       string          `json:"responseId"`
	ErrorMessage     string          `json:"errorMessage"`
}

type radiusToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments any    `json:"arguments"`
}

type radiusUsage struct {
	Input      int `json:"input"`
	Output     int `json:"output"`
	CacheRead  int `json:"cacheRead"`
	CacheWrite int `json:"cacheWrite"`
	Total      int `json:"totalTokens"`
}

type radiusStreamError struct {
	message string
	aborted bool
}

func (err *radiusStreamError) Error() string {
	if err == nil || err.message == "" {
		return "radius messages: gateway stream error"
	}
	return "radius messages: " + err.message
}

type radiusAccumulator struct {
	blocks   map[int]sigma.ContentBlock
	toolArgs map[int]string
}

func newAccumulator() *radiusAccumulator {
	return &radiusAccumulator{blocks: make(map[int]sigma.ContentBlock), toolArgs: make(map[int]string)}
}

func parseStream(ctx context.Context, body io.Reader, writer sigma.StreamWriter, model sigma.Model) (sigma.AssistantMessage, error) {
	final := sigma.AssistantMessage{Model: model.ID, Provider: model.Provider}
	accumulator := newAccumulator()
	terminated := false
	err := sse.Parse(ctx, body, func(frame sse.Event) error {
		if frame.Done || strings.TrimSpace(frame.Data) == "" {
			return nil
		}
		var event radiusEvent
		if err := json.Unmarshal([]byte(frame.Data), &event); err != nil {
			return fmt.Errorf("radius messages: decode stream event: %w", err)
		}
		if err := accumulator.apply(ctx, writer, event); err != nil {
			return err
		}
		switch event.Type {
		case "done":
			terminated = true
			final = accumulator.final(model, event)
			return sse.ErrStop
		case "error":
			terminated = true
			final = accumulator.final(model, event)
			final.StopReason = sigma.StopReasonError
			if event.Reason == "aborted" {
				final.StopReason = sigma.StopReasonAborted
			}
			return &radiusStreamError{message: event.ErrorMessage, aborted: final.StopReason == sigma.StopReasonAborted}
		default:
			return nil
		}
	})
	if err != nil {
		if !terminated {
			return accumulator.final(model, radiusEvent{}), fmt.Errorf("radius messages: parse stream: %w", err)
		}
		return final, fmt.Errorf("radius messages: parse stream: %w", err)
	}
	if !terminated {
		return final, errors.New("radius messages: stream ended without a terminal event")
	}
	return final, nil
}

func (accumulator *radiusAccumulator) apply(ctx context.Context, writer sigma.StreamWriter, event radiusEvent) error {
	index := event.ContentIndex
	switch event.Type {
	case "start":
		return writer.Emit(ctx, sigma.Event{Kind: sigma.EventKindStart})
	case "text_start":
		accumulator.blocks[index] = sigma.ContentBlock{Type: sigma.ContentBlockText}
		return writer.Emit(ctx, sigma.Event{Kind: sigma.EventKindTextStart, ContentIndex: &index})
	case "text_delta":
		block := accumulator.text(index)
		block.Text += event.Delta
		accumulator.blocks[index] = block
		return writer.Emit(ctx, sigma.Event{Kind: sigma.EventKindTextDelta, ContentIndex: &index, DeltaText: event.Delta})
	case "text_end":
		block := accumulator.text(index)
		if event.Content != "" {
			block.Text = event.Content
		}
		block.Signature = event.ContentSignature
		accumulator.blocks[index] = block
		return writer.Emit(ctx, sigma.Event{Kind: sigma.EventKindTextEnd, ContentIndex: &index, Text: block.Text})
	case "thinking_start":
		accumulator.blocks[index] = sigma.ContentBlock{Type: sigma.ContentBlockThinking}
		return writer.Emit(ctx, sigma.Event{Kind: sigma.EventKindThinkingStart, ContentIndex: &index})
	case "thinking_delta":
		block := accumulator.thinking(index)
		block.ThinkingText += event.Delta
		accumulator.blocks[index] = block
		return writer.Emit(ctx, sigma.Event{Kind: sigma.EventKindThinkingDelta, ContentIndex: &index, DeltaText: event.Delta})
	case "thinking_end":
		block := accumulator.thinking(index)
		if event.Content != "" {
			block.ThinkingText = event.Content
		}
		block.Signature = event.ContentSignature
		block.Redacted = event.Redacted
		accumulator.blocks[index] = block
		return writer.Emit(ctx, sigma.Event{Kind: sigma.EventKindThinkingEnd, ContentIndex: &index, Thinking: block.ThinkingText})
	case "toolcall_start":
		accumulator.blocks[index] = sigma.ContentBlock{Type: sigma.ContentBlockToolCall, ToolCallID: event.ID, ToolName: event.ToolName}
		return writer.Emit(ctx, sigma.Event{Kind: sigma.EventKindToolCallStart, ContentIndex: &index, PartialToolCall: &sigma.PartialToolCall{ID: event.ID, Name: event.ToolName}})
	case "toolcall_delta":
		accumulator.toolArgs[index] += event.Delta
		return writer.Emit(ctx, sigma.Event{Kind: sigma.EventKindToolCallDelta, ContentIndex: &index, PartialToolCall: &sigma.PartialToolCall{ArgumentsDelta: event.Delta}})
	case "toolcall_end":
		block := accumulator.tool(index)
		if event.ToolCall != nil {
			block.ToolCallID = event.ToolCall.ID
			block.ToolName = event.ToolCall.Name
			block.ToolArguments = event.ToolCall.Arguments
		} else {
			block.ToolArguments = decodeToolArguments(accumulator.toolArgs[index])
		}
		accumulator.blocks[index] = block
		return writer.Emit(ctx, sigma.Event{Kind: sigma.EventKindToolCallEnd, ContentIndex: &index, ToolCall: &sigma.ToolCall{ID: block.ToolCallID, Name: block.ToolName, Arguments: block.ToolArguments}})
	}
	return nil
}

func (accumulator *radiusAccumulator) text(index int) sigma.ContentBlock {
	block := accumulator.blocks[index]
	if block.Type == "" {
		block.Type = sigma.ContentBlockText
	}
	return block
}

func (accumulator *radiusAccumulator) thinking(index int) sigma.ContentBlock {
	block := accumulator.blocks[index]
	if block.Type == "" {
		block.Type = sigma.ContentBlockThinking
	}
	return block
}

func (accumulator *radiusAccumulator) tool(index int) sigma.ContentBlock {
	block := accumulator.blocks[index]
	if block.Type == "" {
		block.Type = sigma.ContentBlockToolCall
	}
	return block
}

func (accumulator *radiusAccumulator) final(model sigma.Model, event radiusEvent) sigma.AssistantMessage {
	indexes := make([]int, 0, len(accumulator.blocks))
	for index := range accumulator.blocks {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	content := make([]sigma.ContentBlock, 0, len(indexes))
	for _, index := range indexes {
		block := accumulator.blocks[index]
		if block.Type == sigma.ContentBlockToolCall && block.ToolArguments == nil {
			block.ToolArguments = decodeToolArguments(accumulator.toolArgs[index])
		}
		content = append(content, block)
	}
	final := sigma.AssistantMessage{
		Content:    content,
		Model:      model.ID,
		Provider:   model.Provider,
		StopReason: stopReason(event.Reason),
		Usage:      usage(event.Usage, model),
	}
	if event.ResponseID != "" {
		final.ProviderMetadata = map[string]any{"response_id": event.ResponseID}
	}
	return final
}

func decodeToolArguments(arguments string) any {
	if arguments == "" {
		return map[string]any{}
	}
	var decoded any
	if err := json.Unmarshal([]byte(arguments), &decoded); err == nil {
		return decoded
	}
	return arguments
}

func stopReason(reason string) sigma.StopReason {
	switch reason {
	case "stop":
		return sigma.StopReasonEndTurn
	case "length":
		return sigma.StopReasonMaxTokens
	case "toolUse":
		return sigma.StopReasonToolCalls
	case "aborted":
		return sigma.StopReasonAborted
	case "error":
		return sigma.StopReasonError
	default:
		return sigma.StopReasonUnknown
	}
}

func usage(value *radiusUsage, model sigma.Model) *sigma.Usage {
	if value == nil {
		return nil
	}
	return &sigma.Usage{
		InputTokens:           value.Input,
		OutputTokens:          value.Output,
		CacheReadInputTokens:  value.CacheRead,
		CacheWriteInputTokens: value.CacheWrite,
		TotalTokens:           value.Total,
		Provider:              model.Provider,
		Model:                 model.ID,
	}
}

func responseError(resp *http.Response, model sigma.Model) *sigma.ProviderError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return sigma.NewProviderError(
		model.Provider,
		sigma.APIRadiusMessages,
		model.ID,
		resp.StatusCode,
		requestID(resp.Header),
		sigma.RetryAfter(resp.Header),
		body,
		contextOverflowCause(body),
	)
}

func requestID(header http.Header) string {
	for _, key := range []string{"x-request-id", "request-id"} {
		if value := header.Get(key); value != "" {
			return value
		}
	}
	return ""
}

func contextOverflowCause(body []byte) error {
	text := strings.ToLower(string(body))
	if strings.Contains(text, "context") && (strings.Contains(text, "too long") || strings.Contains(text, "maximum") || strings.Contains(text, "exceed")) {
		return sigma.ErrContextOverflow
	}
	return nil
}

func isContextError(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil
}

func abortedError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return &sigma.Error{Code: sigma.ErrorAborted, Message: ctxErr.Error(), Err: ctxErr}
	}
	return &sigma.Error{Code: sigma.ErrorAborted, Message: err.Error(), Err: err}
}
