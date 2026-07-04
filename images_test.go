// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/sigmatest"
)

func TestImageJSONRoundTripPreservesGenerationFields(t *testing.T) {
	t.Parallel()

	req := sigma.ImageRequest{
		Model:    "gpt-image-1",
		Provider: sigma.ProviderOpenAI,
		Prompt:   "A precise product render",
		Inputs: []sigma.ImageInput{
			sigma.ImageText("keep the background white"),
			sigma.ImageData("image/png", "aW5wdXQ="),
		},
		Size:     string(sigma.ImageSize1024x1024),
		Quality:  string(sigma.ImageQualityHigh),
		MIMEType: "image/png",
		Count:    2,
		ProviderMetadata: map[string]any{
			"seed": float64(123),
		},
	}
	roundTrippedReq := assertJSONRoundTrip(t, req)
	if got, want := roundTrippedReq.Quality, string(sigma.ImageQualityHigh); got != want {
		t.Fatalf("quality = %q, want %q", got, want)
	}
	if got, want := roundTrippedReq.Count, 2; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}

	response := sigma.AssistantImages{
		Images: []sigma.ImageInput{
			sigma.ImageOutputData("image/png", "b3V0cHV0"),
			sigma.ImageOutputURL("image/png", "https://example.test/image.png"),
		},
		ResponseID: "img_123",
		StopReason: sigma.StopReasonEndTurn,
		Errors: []sigma.ImageError{{
			Code:    "partial",
			Message: "one candidate was filtered",
			ProviderMetadata: map[string]any{
				"candidate": float64(2),
			},
		}},
		Usage:    &sigma.Usage{InputTokens: 12, OutputTokens: 2, TotalTokens: 14},
		Cost:     &sigma.Cost{TotalCost: 0.02, Currency: "USD"},
		Model:    "gpt-image-1",
		Provider: sigma.ProviderOpenAI,
		ProviderMetadata: map[string]any{
			"request_id": "req_123",
		},
	}
	roundTrippedResponse := assertJSONRoundTrip(t, response)
	if got, want := roundTrippedResponse.ResponseID, "img_123"; got != want {
		t.Fatalf("response id = %q, want %q", got, want)
	}
	if got, want := roundTrippedResponse.StopReason, sigma.StopReasonEndTurn; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := len(roundTrippedResponse.Errors), 1; got != want {
		t.Fatalf("error count = %d, want %d", got, want)
	}
}

func TestGenerateImagesWithFauxProvider(t *testing.T) {
	t.Parallel()

	expected := sigma.AssistantImages{
		Images: []sigma.ImageInput{
			sigma.ImageOutputData("image/png", "aW1hZ2U="),
		},
		ResponseID: "img_test",
		StopReason: sigma.StopReasonEndTurn,
		Errors: []sigma.ImageError{{
			Code:    "warning",
			Message: "kept for response preservation",
		}},
		Usage:    &sigma.Usage{InputTokens: 8, OutputTokens: 1, TotalTokens: 9},
		Model:    sigmatest.ImageModelID,
		Provider: sigmatest.ProviderID,
	}
	provider := sigmatest.NewFauxImageProvider(sigmatest.ImageScript{
		Response: expected,
	})
	registry, err := sigmatest.ImageRegistry(provider)
	if err != nil {
		t.Fatalf("ImageRegistry returned error: %v", err)
	}
	client := sigma.NewClient(
		sigma.WithRegistry(registry),
		sigma.WithDefaultHeader("x-default", "default"),
	)

	req := sigma.ImageRequest{
		Prompt:   "draw a square",
		Size:     string(sigma.ImageSize1024x1024),
		Quality:  string(sigma.ImageQualityMedium),
		MIMEType: "image/png",
		Count:    1,
	}
	got, err := client.GenerateImages(
		context.Background(),
		sigmatest.ImageModel(),
		req,
		sigma.WithImageHeader("x-call", "call"),
		sigma.WithImageMetadataValue("trace", "enabled"),
		sigma.WithImageProviderOption(sigmatest.ProviderID, "payloadHook", "test-hook"),
	)
	if err != nil {
		t.Fatalf("GenerateImages returned error: %v", err)
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("response = %#v, want %#v", got, expected)
	}

	capture, ok := provider.LastRequest()
	if !ok {
		t.Fatal("LastRequest returned no request")
	}
	if got, want := capture.Request.Quality, string(sigma.ImageQualityMedium); got != want {
		t.Fatalf("captured quality = %q, want %q", got, want)
	}
	if got, want := capture.Options.Headers["x-default"], "default"; got != want {
		t.Fatalf("default header = %q, want %q", got, want)
	}
	if got, want := capture.Options.Headers["x-call"], "call"; got != want {
		t.Fatalf("call header = %q, want %q", got, want)
	}
	if got, want := capture.Options.Metadata["trace"], "enabled"; got != want {
		t.Fatalf("metadata trace = %v, want %v", got, want)
	}
	if got, want := capture.Options.ProviderOptions[sigmatest.ProviderID]["payloadHook"], "test-hook"; got != want {
		t.Fatalf("provider option = %v, want %v", got, want)
	}
}

func TestGenerateImagesMissingImageProvider(t *testing.T) {
	t.Parallel()

	registry, err := sigmatest.Registry(sigmatest.NewFauxProvider())
	if err != nil {
		t.Fatalf("Registry returned error: %v", err)
	}
	if err := registry.RegisterImageModel(sigmatest.ImageModel(), sigma.WithMetadataOnly()); err != nil {
		t.Fatalf("RegisterImageModel returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))

	response, err := client.GenerateImages(context.Background(), sigmatest.ImageModel(), sigma.ImageRequest{})
	if err == nil {
		t.Fatal("GenerateImages returned nil error")
	}
	if !errors.Is(err, sigma.ErrNoProvider) {
		t.Fatalf("error = %v, want ErrNoProvider", err)
	}
	var sigmaErr *sigma.Error
	if !errors.As(err, &sigmaErr) {
		t.Fatalf("error type = %T, want *sigma.Error", err)
	}
	if got, want := sigmaErr.Code, sigma.ErrorProviderNotFound; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
	if got, want := response.StopReason, sigma.StopReasonError; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
}

func TestGenerateImagesCancellation(t *testing.T) {
	t.Parallel()

	provider := sigmatest.NewFauxImageProvider(sigmatest.ImageScript{WaitForCancel: true})
	registry, err := sigmatest.ImageRegistry(provider)
	if err != nil {
		t.Fatalf("ImageRegistry returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))
	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(10*time.Millisecond, cancel)
	defer timer.Stop()

	response, err := client.GenerateImages(ctx, sigmatest.ImageModel(), sigma.ImageRequest{})
	if err == nil {
		t.Fatal("GenerateImages returned nil error")
	}
	if !errors.Is(err, sigma.ErrAborted) {
		t.Fatalf("error = %v, want ErrAborted", err)
	}
	if got, want := response.StopReason, sigma.StopReasonAborted; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := response.Model, sigmatest.ImageModelID; got != want {
		t.Fatalf("model = %q, want %q", got, want)
	}
}

func TestCollectImagesWithCanceledContextReturnsPartialFinalImages(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	stream, writer := sigma.NewImageStream(ctx)
	image := sigma.ImageOutputData("image/png", "aW1hZ2U=")
	if err := writer.Emit(context.Background(), sigma.ImageEvent{
		Kind:  sigma.ImageEventKindImage,
		Image: &image,
	}); err != nil {
		t.Fatalf("Emit returned error: %v", err)
	}
	cancel()

	final, err := sigma.CollectImages(ctx, stream)
	if err == nil {
		t.Fatal("CollectImages returned nil error")
	}
	if !errors.Is(err, sigma.ErrAborted) {
		t.Fatalf("CollectImages error = %v, want ErrAborted", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CollectImages error = %v, want context.Canceled", err)
	}
	if got, want := final.StopReason, sigma.StopReasonAborted; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := len(final.Images), 1; got != want {
		t.Fatalf("image count = %d, want %d", got, want)
	}
	if got, want := final.Images[0].Data, "aW1hZ2U="; got != want {
		t.Fatalf("image data = %q, want %q", got, want)
	}
}
