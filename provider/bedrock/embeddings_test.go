// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package bedrock

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

type capturedEmbeddingRequest struct {
	Path          string
	Authorization string
	Body          string
}

func TestRegisterReportsEmbeddingsAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	providerID := sigma.ProviderID("bedrock-embeddings-compatible")
	if err := RegisterEmbeddings(registry, providerID, WithCredentialDetector(fakeCredentialDetector{})); err != nil {
		t.Fatalf("RegisterEmbeddings returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := providers[0].EmbeddingAPI, sigma.EmbeddingAPIBedrockEmbeddings; got != want {
		t.Fatalf("embedding API = %q, want %q", got, want)
	}
}

func TestTitanV2EmbeddingsInvokeModelPayloadAndResponse(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedEmbeddingRequest, 1)
	server := bedrockEmbeddingServer(t, requests, `{"embedding":[0.1,0.2],"inputTextTokenCount":5}`)
	defer server.Close()

	model := bedrockEmbeddingModel("amazon.titan-embed-text-v2:0")
	client := bedrockEmbeddingTestClient(t, model, server.URL)
	got, err := client.Embed(
		context.Background(),
		model,
		sigma.EmbeddingRequest{Inputs: []string{"alpha"}, Dimensions: 512},
		sigma.WithEmbeddingProviderOption(sigma.ProviderAmazonBedrock, "embeddingTypes", []string{"float"}),
	)
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}
	if gotVectors := got.Vectors; !reflect.DeepEqual(gotVectors, []sigma.Embedding{{Index: 0, Vector: []float32{0.1, 0.2}}}) {
		t.Fatalf("vectors = %#v", gotVectors)
	}
	if got.Usage == nil || got.Usage.InputTokens != 5 || got.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %#v, want input token count", got.Usage)
	}

	request := receiveEmbeddingRequest(t, requests)
	if gotPath, want := request.Path, "/model/amazon.titan-embed-text-v2:0/invoke"; gotPath != want {
		t.Fatalf("path = %q, want %q", gotPath, want)
	}
	if got, want := request.Authorization, "Bearer bedrock-token"; got != want {
		t.Fatalf("authorization = %q, want %q", got, want)
	}
	payload := decodeEmbeddingPayload(t, request.Body)
	if gotText, want := payload["inputText"], "alpha"; gotText != want {
		t.Fatalf("inputText = %v, want %q", gotText, want)
	}
	if gotDimensions, want := payload["dimensions"], float64(512); gotDimensions != want {
		t.Fatalf("dimensions = %v, want %v", gotDimensions, want)
	}
	if gotNormalize, want := payload["normalize"], true; gotNormalize != want {
		t.Fatalf("normalize = %v, want %v", gotNormalize, want)
	}
}

func TestBedrockEmbeddingRequestSignsInferenceProfileARNWithEscapedPath(t *testing.T) {
	t.Parallel()

	const arn = "arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/my-profile"
	body := []byte(`{"inputText":"alpha"}`)
	req, err := bedrockEmbeddingRequest(
		context.Background(),
		Config{Region: "us-east-1", Endpoint: "https://bedrock-runtime.us-east-1.amazonaws.com"},
		arn,
		body,
		sigma.Options{},
		CredentialInfo{
			Source:          CredentialSourceStaticCredentials,
			AccessKeyID:     "AKIAFAKE",
			SecretAccessKey: "secret",
		},
	)
	if err != nil {
		t.Fatalf("bedrockEmbeddingRequest returned error: %v", err)
	}
	wantPath := "/model/" + url.PathEscape(arn) + "/invoke"
	if got := req.URL.EscapedPath(); got != wantPath {
		t.Fatalf("escaped path = %q, want %q", got, wantPath)
	}
	assertBedrockSigV4Signature(t, req, body, wantPath, "secret", "us-east-1", "bedrock")
}

func TestCohereEmbeddingsBatchPayloadAndNestedVectors(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedEmbeddingRequest, 1)
	server := bedrockEmbeddingServer(t, requests, `{"embeddings":{"float":[[0.1],[0.2]]},"meta":{"billed_units":{"input_tokens":4}}}`)
	defer server.Close()

	model := bedrockEmbeddingModel("cohere.embed-english-v3")
	client := bedrockEmbeddingTestClient(t, model, server.URL)
	got, err := client.Embed(
		context.Background(),
		model,
		sigma.EmbeddingRequest{Inputs: []string{"alpha", "beta"}, InputType: sigma.EmbeddingInputTypeQuery},
	)
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}
	if gotVectors := got.Vectors; !reflect.DeepEqual(gotVectors, []sigma.Embedding{{Index: 0, Vector: []float32{0.1}}, {Index: 1, Vector: []float32{0.2}}}) {
		t.Fatalf("vectors = %#v", gotVectors)
	}

	request := receiveEmbeddingRequest(t, requests)
	payload := decodeEmbeddingPayload(t, request.Body)
	if got, want := payload["input_type"], "search_query"; got != want {
		t.Fatalf("input_type = %v, want %q", got, want)
	}
	texts := payload["texts"].([]any)
	if len(texts) != 2 || texts[0] != "alpha" || texts[1] != "beta" {
		t.Fatalf("texts = %#v", texts)
	}
}

func TestNovaEmbeddingPayloadAndResponse(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedEmbeddingRequest, 1)
	server := bedrockEmbeddingServer(t, requests, `{"embeddings":[{"embedding":[0.7,0.8]}]}`)
	defer server.Close()

	model := bedrockEmbeddingModel("amazon.nova-2-multimodal-embeddings-v1:0")
	client := bedrockEmbeddingTestClient(t, model, server.URL)
	got, err := client.Embed(
		context.Background(),
		model,
		sigma.EmbeddingRequest{Inputs: []string{"alpha"}},
		sigma.WithEmbeddingProviderOption(sigma.ProviderAmazonBedrock, "embeddingDimension", 1024),
	)
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}
	if gotVectors := got.Vectors; !reflect.DeepEqual(gotVectors, []sigma.Embedding{{Index: 0, Vector: []float32{0.7, 0.8}}}) {
		t.Fatalf("vectors = %#v", gotVectors)
	}

	payload := decodeEmbeddingPayload(t, receiveEmbeddingRequest(t, requests).Body)
	if got, want := payload["schemaVersion"], "nova-multimodal-embed-v1"; got != want {
		t.Fatalf("schemaVersion = %v, want %q", got, want)
	}
	params := payload["singleEmbeddingParams"].(map[string]any)
	if got, want := params["embeddingDimension"], float64(1024); got != want {
		t.Fatalf("embeddingDimension = %v, want %v", got, want)
	}
	text := params["text"].(map[string]any)
	if got, want := text["value"], "alpha"; got != want {
		t.Fatalf("text.value = %v, want %q", got, want)
	}
}

func TestTitanImageEmbeddingUsesTextOnlyPath(t *testing.T) {
	t.Parallel()

	model := bedrockEmbeddingModel("amazon.titan-embed-image-v1")
	payload, err := bedrockEmbeddingPayload(
		string(model.ID),
		model,
		sigma.EmbeddingRequest{Inputs: []string{"caption"}},
		sigma.Options{ProviderOptions: map[sigma.ProviderID]map[string]any{
			sigma.ProviderAmazonBedrock: {"outputEmbeddingLength": 384},
		}},
	)
	if err != nil {
		t.Fatalf("bedrockEmbeddingPayload returned error: %v", err)
	}
	if got, want := payload["inputText"], "caption"; got != want {
		t.Fatalf("inputText = %v, want %q", got, want)
	}
	config := payload["embeddingConfig"].(map[string]any)
	if got, want := config["outputEmbeddingLength"], 384; got != want {
		t.Fatalf("outputEmbeddingLength = %v, want %v", got, want)
	}
}

func TestBedrockEmbeddingsProviderErrorIsTyped(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedEmbeddingRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureEmbeddingRequest(t, requests, r)
		w.Header().Set("x-amzn-requestid", "req-123")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"message":"bad request"}`)
	}))
	defer server.Close()

	model := bedrockEmbeddingModel("amazon.titan-embed-text-v2:0")
	client := bedrockEmbeddingTestClient(t, model, server.URL)
	response, err := client.Embed(context.Background(), model, sigma.EmbeddingRequest{Inputs: []string{"alpha"}})
	if err == nil {
		t.Fatal("Embed returned nil error")
	}
	if len(response.Vectors) != 0 {
		t.Fatalf("vectors = %#v, want none", response.Vectors)
	}
	var providerErr *sigma.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error type = %T, want *sigma.ProviderError", err)
	}
	if got, want := providerErr.API, sigma.API(sigma.EmbeddingAPIBedrockEmbeddings); got != want {
		t.Fatalf("provider error API = %q, want %q", got, want)
	}
	if !strings.Contains(err.Error(), "bad request") {
		t.Fatalf("error = %v, want provider body", err)
	}
}

func bedrockEmbeddingServer(t *testing.T, requests chan<- capturedEmbeddingRequest, response string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureEmbeddingRequest(t, requests, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, response)
	}))
}

func captureEmbeddingRequest(t *testing.T, requests chan<- capturedEmbeddingRequest, r *http.Request) {
	t.Helper()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("ReadAll request body returned error: %v", err)
	}
	requests <- capturedEmbeddingRequest{
		Path:          r.URL.Path,
		Authorization: r.Header.Get("Authorization"),
		Body:          string(body),
	}
}

func receiveEmbeddingRequest(t *testing.T, requests <-chan capturedEmbeddingRequest) capturedEmbeddingRequest {
	t.Helper()

	select {
	case request := <-requests:
		return request
	default:
		t.Fatal("server did not receive request")
		return capturedEmbeddingRequest{}
	}
}

func bedrockEmbeddingTestClient(t *testing.T, model sigma.EmbeddingModel, endpoint string) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	if err := registry.RegisterEmbeddingProvider(
		model.Provider,
		NewEmbeddingsProvider(
			WithEndpoint(endpoint),
			WithRegion("us-east-1"),
			WithCredentialDetector(fakeCredentialDetector{info: CredentialInfo{Source: CredentialSourceAuthResolver, BearerToken: "bedrock-token"}}),
		),
	); err != nil {
		t.Fatalf("RegisterEmbeddingProvider returned error: %v", err)
	}
	if err := registry.RegisterEmbeddingModel(model); err != nil {
		t.Fatalf("RegisterEmbeddingModel returned error: %v", err)
	}
	return sigma.NewClient(sigma.WithRegistry(registry))
}

func bedrockEmbeddingModel(id sigma.ModelID) sigma.EmbeddingModel {
	return sigma.EmbeddingModel{
		ID:                  id,
		Provider:            sigma.ProviderAmazonBedrock,
		API:                 sigma.EmbeddingAPIBedrockEmbeddings,
		InputCostPerMillion: 1,
		CostCurrency:        "USD",
	}
}

func decodeEmbeddingPayload(t *testing.T, body string) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode request payload: %v", err)
	}
	return payload
}
