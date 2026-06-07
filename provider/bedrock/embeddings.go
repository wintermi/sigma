// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package bedrock

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/wintermi/sigma"
)

const (
	bedrockEmbeddingOptionDimensions                 = "dimensions"
	bedrockEmbeddingOptionNormalize                  = "normalize"
	bedrockEmbeddingOptionEmbeddingTypes             = "embeddingTypes"
	bedrockEmbeddingOptionEmbeddingTypesSnake        = "embedding_types"
	bedrockEmbeddingOptionOutputEmbeddingLength      = "outputEmbeddingLength"
	bedrockEmbeddingOptionOutputEmbeddingLengthSnake = "output_embedding_length"
	bedrockEmbeddingOptionEmbeddingPurpose           = "embeddingPurpose"
	bedrockEmbeddingOptionEmbeddingPurposeSnake      = "embedding_purpose"
	bedrockEmbeddingOptionEmbeddingDimension         = "embeddingDimension"
	bedrockEmbeddingOptionEmbeddingDimensionSnake    = "embedding_dimension"
	bedrockEmbeddingOptionTruncationMode             = "truncationMode"
	bedrockEmbeddingOptionTruncationModeSnake        = "truncation_mode"
	bedrockEmbeddingOptionInputType                  = "input_type"
	bedrockEmbeddingOptionTruncate                   = "truncate"
	bedrockEmbeddingOptionOutputDimension            = "output_dimension"
)

// EmbeddingsProvider adapts Bedrock InvokeModel text embeddings to sigma.
type EmbeddingsProvider struct {
	base *Provider
}

// NewEmbeddingsProvider constructs a Bedrock embeddings provider.
func NewEmbeddingsProvider(opts ...ProviderOption) *EmbeddingsProvider {
	return &EmbeddingsProvider{base: NewProvider(opts...)}
}

// RegisterEmbeddings adds a Bedrock embeddings provider to registry.
func RegisterEmbeddings(registry *sigma.Registry, providerID sigma.ProviderID, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	if err := registry.RegisterEmbeddingProvider(providerID, NewEmbeddingsProvider(opts...)); err != nil {
		return fmt.Errorf("bedrock embeddings: register provider: %w", err)
	}
	return nil
}

// RegisterEmbeddingsDefault adds a Bedrock embeddings provider to sigma's default registry.
func RegisterEmbeddingsDefault(providerID sigma.ProviderID, opts ...ProviderOption) error {
	if err := sigma.RegisterDefaultEmbeddingProvider(providerID, NewEmbeddingsProvider(opts...)); err != nil {
		return fmt.Errorf("bedrock embeddings: register default provider: %w", err)
	}
	return nil
}

// API reports the Bedrock embeddings API surface.
func (p *EmbeddingsProvider) API() sigma.EmbeddingAPI {
	return sigma.EmbeddingAPIBedrockEmbeddings
}

// Embed sends req to Bedrock Runtime InvokeModel.
func (p *EmbeddingsProvider) Embed(ctx context.Context, model sigma.EmbeddingModel, req sigma.EmbeddingRequest, opts sigma.Options) (sigma.Embeddings, error) {
	ctx, cancel := sigma.ContextWithRequestTimeout(ctx, opts)
	defer cancel()

	textModel := bedrockEmbeddingAuthModel(model)
	effective := effectiveConfig(p.base.config, textModel, opts)
	modelID := bedrockEmbeddingModelID(effective, model.ID)
	payload, err := bedrockEmbeddingPayload(modelID, model, req, opts)
	if err != nil {
		return sigma.Embeddings{Model: model.ID, Provider: model.Provider}, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return sigma.Embeddings{Model: model.ID, Provider: model.Provider}, fmt.Errorf("bedrock embeddings: encode request: %w", err)
	}
	if len(opts.EmbeddingPayloadDebugHooks) > 0 {
		if err := sigma.RunEmbeddingPayloadDebugHooks(ctx, opts, model.Provider, sigma.EmbeddingAPIBedrockEmbeddings, model.ID, body, nil); err != nil {
			return sigma.Embeddings{Model: model.ID, Provider: model.Provider}, fmt.Errorf("bedrock embeddings: payload debug hooks: %w", err)
		}
	}

	credentials, err := p.base.credentialDetector.Detect(ctx, textModel, opts, effective)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return sigma.Embeddings{Model: model.ID, Provider: model.Provider}, contextError(ctx, err)
		}
		return sigma.Embeddings{Model: model.ID, Provider: model.Provider}, fmt.Errorf("bedrock embeddings: credentials: %w", err)
	}

	resp, attempts, err := sigma.DoHTTPWithRetryAttempts(
		ctx,
		bedrockHTTPClient(opts),
		opts,
		func(ctx context.Context) (*http.Request, error) {
			return bedrockEmbeddingRequest(ctx, effective, modelID, body, opts, credentials)
		},
		func(resp *http.Response) *sigma.ProviderError {
			return bedrockEmbeddingsResponseError(resp, model)
		},
		sigma.EmbeddingResponseDebugHTTPHook(ctx, opts, model.Provider, sigma.EmbeddingAPIBedrockEmbeddings, model.ID),
	)
	embeddingAttempts := bedrockEmbeddingAttemptsFromHTTP(model, attempts)
	if err != nil {
		response := sigma.Embeddings{Model: model.ID, Provider: model.Provider, Attempts: embeddingAttempts}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return response, contextError(ctx, err)
		}
		return response, fmt.Errorf("bedrock embeddings: request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		response := sigma.Embeddings{Model: model.ID, Provider: model.Provider, Attempts: embeddingAttempts}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return response, contextError(ctx, err)
		}
		return response, fmt.Errorf("bedrock embeddings: read response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return sigma.Embeddings{Model: model.ID, Provider: model.Provider, Attempts: embeddingAttempts}, bedrockEmbeddingsProviderError(resp, model, respBody, sigma.ErrProviderResponse)
	}
	embeddings, err := decodeBedrockEmbeddingResponse(modelID, model, req, respBody)
	embeddings.Attempts = embeddingAttempts
	return embeddings, err
}

func bedrockEmbeddingPayload(modelID string, model sigma.EmbeddingModel, req sigma.EmbeddingRequest, opts sigma.Options) (map[string]any, error) {
	switch {
	case strings.Contains(modelID, "cohere"):
		return cohereEmbeddingPayload(model, req, opts), nil
	case strings.Contains(modelID, "nova"):
		if len(req.Inputs) != 1 {
			return nil, fmt.Errorf("bedrock embeddings: model %s supports one input per request", modelID)
		}
		return novaEmbeddingPayload(model, req.Inputs[0], opts), nil
	case strings.HasPrefix(modelID, "amazon.titan-embed-image"):
		if len(req.Inputs) != 1 {
			return nil, fmt.Errorf("bedrock embeddings: model %s supports one input per request", modelID)
		}
		return titanImageEmbeddingPayload(model, req.Inputs[0], opts), nil
	default:
		if len(req.Inputs) != 1 {
			return nil, fmt.Errorf("bedrock embeddings: model %s supports one input per request", modelID)
		}
		return titanTextEmbeddingPayload(modelID, model, req.Inputs[0], req, opts), nil
	}
}

func titanTextEmbeddingPayload(modelID string, model sigma.EmbeddingModel, input string, req sigma.EmbeddingRequest, opts sigma.Options) map[string]any {
	payload := map[string]any{"inputText": input}
	options := providerOptions(opts, model.Provider)
	if strings.Contains(modelID, "titan-embed-text-v2") {
		addTitanV2EmbeddingOptions(payload, req, options)
	}
	for key, value := range req.ProviderMetadata {
		payload[key] = value
	}
	return payload
}

func addTitanV2EmbeddingOptions(payload map[string]any, req sigma.EmbeddingRequest, options map[string]any) {
	payload["normalize"] = true
	if req.Dimensions > 0 {
		payload["dimensions"] = req.Dimensions
	}
	if value, ok := intBedrockOption(options, bedrockEmbeddingOptionDimensions); ok {
		payload["dimensions"] = value
	}
	if value, ok := boolOption(options, bedrockEmbeddingOptionNormalize); ok {
		payload["normalize"] = value
	}
	if values, ok := stringSliceOption(options, bedrockEmbeddingOptionEmbeddingTypes); ok {
		payload["embeddingTypes"] = values
		return
	}
	if values, ok := stringSliceOption(options, bedrockEmbeddingOptionEmbeddingTypesSnake); ok {
		payload["embeddingTypes"] = values
	}
}

func titanImageEmbeddingPayload(model sigma.EmbeddingModel, input string, opts sigma.Options) map[string]any {
	options := providerOptions(opts, model.Provider)
	length := 1024
	if value, ok := intBedrockOption(options, bedrockEmbeddingOptionOutputEmbeddingLength); ok {
		length = value
	} else if value, ok := intBedrockOption(options, bedrockEmbeddingOptionOutputEmbeddingLengthSnake); ok {
		length = value
	}
	return map[string]any{
		"inputText": input,
		"embeddingConfig": map[string]any{
			"outputEmbeddingLength": length,
		},
	}
}

func novaEmbeddingPayload(model sigma.EmbeddingModel, input string, opts sigma.Options) map[string]any {
	options := providerOptions(opts, model.Provider)
	purpose := "GENERIC_INDEX"
	if value, ok := stringOption(options, bedrockEmbeddingOptionEmbeddingPurpose); ok {
		purpose = value
	} else if value, ok := stringOption(options, bedrockEmbeddingOptionEmbeddingPurposeSnake); ok {
		purpose = value
	}
	dimension := 3072
	if value, ok := intBedrockOption(options, bedrockEmbeddingOptionEmbeddingDimension); ok {
		dimension = value
	} else if value, ok := intBedrockOption(options, bedrockEmbeddingOptionEmbeddingDimensionSnake); ok {
		dimension = value
	}
	truncation := "END"
	if value, ok := stringOption(options, bedrockEmbeddingOptionTruncationMode); ok {
		truncation = value
	} else if value, ok := stringOption(options, bedrockEmbeddingOptionTruncationModeSnake); ok {
		truncation = value
	}
	return map[string]any{
		"schemaVersion": "nova-multimodal-embed-v1",
		"taskType":      "SINGLE_EMBEDDING",
		"singleEmbeddingParams": map[string]any{
			"embeddingPurpose":   purpose,
			"embeddingDimension": dimension,
			"text": map[string]any{
				"truncationMode": truncation,
				"value":          input,
			},
		},
	}
}

func cohereEmbeddingPayload(model sigma.EmbeddingModel, req sigma.EmbeddingRequest, opts sigma.Options) map[string]any {
	inputType := "search_document"
	if req.InputType == sigma.EmbeddingInputTypeQuery {
		inputType = "search_query"
	}
	options := providerOptions(opts, model.Provider)
	if value, ok := stringOption(options, bedrockEmbeddingOptionInputType); ok {
		inputType = value
	}
	payload := map[string]any{
		"texts":      append([]string(nil), req.Inputs...),
		"input_type": inputType,
	}
	for _, key := range []string{bedrockEmbeddingOptionTruncate, bedrockEmbeddingOptionOutputDimension, bedrockEmbeddingOptionEmbeddingTypesSnake} {
		if value, ok := options[key]; ok {
			payload[key] = value
		}
	}
	for key, value := range req.ProviderMetadata {
		payload[key] = value
	}
	return payload
}

func bedrockEmbeddingRequest(ctx context.Context, config Config, modelID string, body []byte, opts sigma.Options, credentials CredentialInfo) (*http.Request, error) {
	if credentials.BearerToken == "" && config.Region == "" {
		return nil, &sigma.Error{
			Code:    sigma.ErrorUnsupported,
			Message: "bedrock embeddings: region is required",
		}
	}
	endpoint := bedrockInvokeModelURL(config, modelID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", "sigma/bedrock-embeddings")
	for key, value := range opts.Headers {
		if !reservedBedrockHeader(key) {
			httpReq.Header.Set(key, value)
		}
	}
	if credentials.BearerToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+credentials.BearerToken)
	} else {
		signAWSSigV4(httpReq, body, credentials.AccessKeyID, credentials.SecretAccessKey, credentials.SessionToken, config.Region, "bedrock")
	}
	return httpReq, nil
}

func bedrockInvokeModelURL(config Config, modelID string) string {
	base := strings.TrimRight(config.Endpoint, "/")
	if base == "" {
		base = fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", config.Region)
	}
	return base + "/model/" + url.PathEscape(modelID) + "/invoke"
}

func decodeBedrockEmbeddingResponse(modelID string, model sigma.EmbeddingModel, req sigma.EmbeddingRequest, body []byte) (sigma.Embeddings, error) {
	switch {
	case strings.Contains(modelID, "cohere"):
		return decodeCohereEmbeddingResponse(model, len(req.Inputs), body)
	case strings.Contains(modelID, "nova"):
		return decodeNovaEmbeddingResponse(model, body)
	default:
		return decodeTitanEmbeddingResponse(model, body)
	}
}

type titanEmbeddingResponse struct {
	Embedding           []float32 `json:"embedding"`
	InputTextTokenCount int       `json:"inputTextTokenCount"`
}

func decodeTitanEmbeddingResponse(model sigma.EmbeddingModel, body []byte) (sigma.Embeddings, error) {
	var decoded titanEmbeddingResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return sigma.Embeddings{}, fmt.Errorf("bedrock embeddings: decode Titan response: %w", err)
	}
	out := sigma.Embeddings{
		Model:    model.ID,
		Provider: model.Provider,
		Vectors:  []sigma.Embedding{{Index: 0, Vector: append([]float32(nil), decoded.Embedding...)}},
	}
	if decoded.InputTextTokenCount > 0 {
		addEmbeddingUsage(&out, model, decoded.InputTextTokenCount)
	}
	return out, nil
}

type novaEmbeddingResponse struct {
	Embeddings []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"embeddings"`
}

func decodeNovaEmbeddingResponse(model sigma.EmbeddingModel, body []byte) (sigma.Embeddings, error) {
	var decoded novaEmbeddingResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return sigma.Embeddings{}, fmt.Errorf("bedrock embeddings: decode Nova response: %w", err)
	}
	if len(decoded.Embeddings) == 0 {
		return sigma.Embeddings{}, fmt.Errorf("bedrock embeddings: Nova returned no embeddings")
	}
	return sigma.Embeddings{
		Model:    model.ID,
		Provider: model.Provider,
		Vectors:  []sigma.Embedding{{Index: 0, Vector: append([]float32(nil), decoded.Embeddings[0].Embedding...)}},
	}, nil
}

type cohereEmbeddingResponse struct {
	Embeddings json.RawMessage `json:"embeddings"`
	Meta       struct {
		BilledUnits struct {
			InputTokens int `json:"input_tokens"`
		} `json:"billed_units"`
	} `json:"meta"`
}

func decodeCohereEmbeddingResponse(model sigma.EmbeddingModel, inputCount int, body []byte) (sigma.Embeddings, error) {
	var decoded cohereEmbeddingResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return sigma.Embeddings{}, fmt.Errorf("bedrock embeddings: decode Cohere response: %w", err)
	}
	vectors, err := parseCohereEmbeddings(decoded.Embeddings)
	if err != nil {
		return sigma.Embeddings{}, fmt.Errorf("bedrock embeddings: decode Cohere vectors: %w", err)
	}
	if len(vectors) != inputCount {
		return sigma.Embeddings{}, fmt.Errorf("bedrock embeddings: provider returned %d vectors for %d inputs", len(vectors), inputCount)
	}
	out := sigma.Embeddings{
		Model:    model.ID,
		Provider: model.Provider,
		Vectors:  make([]sigma.Embedding, 0, len(vectors)),
	}
	for index, vector := range vectors {
		out.Vectors = append(out.Vectors, sigma.Embedding{Index: index, Vector: vector})
	}
	if decoded.Meta.BilledUnits.InputTokens > 0 {
		addEmbeddingUsage(&out, model, decoded.Meta.BilledUnits.InputTokens)
	}
	return out, nil
}

func parseCohereEmbeddings(raw json.RawMessage) ([][]float32, error) {
	var flat [][]float32
	if err := json.Unmarshal(raw, &flat); err == nil {
		return flat, nil
	}
	var nested struct {
		Float [][]float32 `json:"float"`
	}
	if err := json.Unmarshal(raw, &nested); err != nil {
		return nil, errors.New("unrecognised embeddings format")
	}
	if len(nested.Float) == 0 {
		return nil, errors.New("no float embeddings in response")
	}
	return nested.Float, nil
}

func addEmbeddingUsage(out *sigma.Embeddings, model sigma.EmbeddingModel, tokens int) {
	usage := sigma.Usage{InputTokens: tokens, TotalTokens: tokens}
	out.Usage = &usage
	cost := sigma.CostForEmbeddingUsage(model, usage)
	out.Cost = &cost
}

func bedrockEmbeddingModelID(config Config, modelID sigma.ModelID) string {
	switch {
	case config.InferenceProfileARN != "":
		return config.InferenceProfileARN
	case config.ModelID != "":
		return config.ModelID
	default:
		return string(modelID)
	}
}

func bedrockHTTPClient(opts sigma.Options) *http.Client {
	if opts.HTTPClient != nil {
		return opts.HTTPClient
	}
	return http.DefaultClient
}

func bedrockEmbeddingAuthModel(model sigma.EmbeddingModel) sigma.Model {
	return sigma.Model{
		ID:                  model.ID,
		Provider:            model.Provider,
		API:                 sigma.API(sigma.EmbeddingAPIBedrockEmbeddings),
		Name:                model.Name,
		ContextWindow:       model.MaxInputTokens,
		InputCostPerMillion: model.InputCostPerMillion,
		CostCurrency:        model.CostCurrency,
		ProviderMetadata:    model.ProviderMetadata,
	}
}

func bedrockEmbeddingsResponseError(resp *http.Response, model sigma.EmbeddingModel) *sigma.ProviderError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return bedrockEmbeddingsProviderError(resp, model, body, sigma.ErrProviderResponse)
}

func bedrockEmbeddingsProviderError(resp *http.Response, model sigma.EmbeddingModel, body []byte, err error) *sigma.ProviderError {
	return sigma.NewProviderError(
		model.Provider,
		sigma.API(sigma.EmbeddingAPIBedrockEmbeddings),
		model.ID,
		resp.StatusCode,
		resp.Header.Get("x-amzn-requestid"),
		sigma.RetryAfter(resp.Header),
		body,
		err,
	)
}

func bedrockEmbeddingAttemptsFromHTTP(model sigma.EmbeddingModel, attempts []sigma.HTTPAttempt) []sigma.EmbeddingAttempt {
	if len(attempts) == 0 {
		return nil
	}
	out := make([]sigma.EmbeddingAttempt, 0, len(attempts))
	for _, attempt := range attempts {
		out = append(out, sigma.EmbeddingAttempt{
			Provider:   model.Provider,
			API:        sigma.EmbeddingAPIBedrockEmbeddings,
			Model:      model.ID,
			Attempt:    attempt.Attempt,
			StatusCode: attempt.StatusCode,
			RequestID:  attempt.RequestID,
			Latency:    attempt.Latency,
		})
	}
	return out
}

func intBedrockOption(options map[string]any, key string) (int, bool) {
	switch value := options[key].(type) {
	case int:
		return value, true
	case int32:
		return int(value), true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	case json.Number:
		parsed, err := value.Int64()
		return int(parsed), err == nil
	default:
		return 0, false
	}
}
