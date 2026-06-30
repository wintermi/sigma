// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"testing"

	"github.com/wintermi/sigma"
)

func TestModelThinkingLevelMapControlsSupportedLevels(t *testing.T) {
	t.Parallel()

	model := sigma.Model{
		ThinkingLevelMap: map[sigma.ThinkingLevel]string{
			sigma.ThinkingLevelMinimal: "minimal",
			sigma.ThinkingLevelHigh:    "high",
		},
	}

	if !model.SupportsReasoning() {
		t.Fatal("model did not report reasoning support")
	}
	if !model.SupportsThinkingLevel(sigma.ThinkingLevelMinimal) {
		t.Fatal("minimal thinking level was not supported")
	}
	if model.SupportsThinkingLevel(sigma.ThinkingLevelMedium) {
		t.Fatal("medium thinking level was supported without metadata")
	}

	value, ok := model.ProviderThinkingLevel(sigma.ThinkingLevelHigh)
	if !ok {
		t.Fatal("high thinking level was not translated")
	}
	if got, want := value, "high"; got != want {
		t.Fatalf("provider thinking level = %q, want %q", got, want)
	}
}

func TestModelThinkingLevelsRejectUnsupportedLevels(t *testing.T) {
	t.Parallel()

	model := sigma.Model{
		ThinkingLevels: []sigma.ThinkingLevel{
			sigma.ThinkingLevelLow,
			sigma.ThinkingLevelXHigh,
		},
	}

	if !model.SupportsThinkingLevel(sigma.ThinkingLevelLow) {
		t.Fatal("low thinking level was not supported")
	}
	if !model.SupportsThinkingLevel(sigma.ThinkingLevelOff) {
		t.Fatal("off thinking level should be accepted")
	}
	if model.SupportsThinkingLevel(sigma.ThinkingLevelHigh) {
		t.Fatal("high thinking level was supported without metadata")
	}

	value, ok := model.ProviderThinkingLevel(sigma.ThinkingLevelXHigh)
	if !ok {
		t.Fatal("xhigh thinking level was not translated")
	}
	if got, want := value, "xhigh"; got != want {
		t.Fatalf("provider thinking level = %q, want %q", got, want)
	}
}

func TestModelUnsupportedThinkingLevelsOverrideDefaults(t *testing.T) {
	t.Parallel()

	model := sigma.Model{
		SupportsThinking: true,
		ThinkingLevelMap: map[sigma.ThinkingLevel]string{
			sigma.ThinkingLevelHigh: "high",
		},
		UnsupportedThinkingLevels: []sigma.ThinkingLevel{
			sigma.ThinkingLevelOff,
			sigma.ThinkingLevelLow,
		},
	}

	if model.SupportsThinkingLevel(sigma.ThinkingLevelOff) {
		t.Fatal("off thinking level was supported despite unsupported metadata")
	}
	if model.SupportsThinkingLevel(sigma.ThinkingLevelLow) {
		t.Fatal("low thinking level was supported despite unsupported metadata")
	}
	if !model.SupportsThinkingLevel(sigma.ThinkingLevelHigh) {
		t.Fatal("high thinking level was not supported")
	}
	if value, ok := model.ProviderThinkingLevel(sigma.ThinkingLevelOff); ok || value != "" {
		t.Fatalf("provider off thinking level = %q, %v; want empty, false", value, ok)
	}
}

func TestModelInputCapabilities(t *testing.T) {
	t.Parallel()

	textOnly := sigma.Model{MaxOutputTokens: 4096}
	if !textOnly.SupportsInput(sigma.ContentBlockText) {
		t.Fatal("model without explicit inputs should support text")
	}
	if textOnly.SupportsImages() {
		t.Fatal("model without explicit image input support reported SupportsImages")
	}
	if textOnly.SupportsDocuments() {
		t.Fatal("model without explicit document input support reported SupportsDocuments")
	}
	if got, want := textOnly.MaxOutputTokens, 4096; got != want {
		t.Fatalf("max output tokens = %d, want %d", got, want)
	}

	multimodal := sigma.Model{
		SupportedInputs: []sigma.ContentBlockType{
			sigma.ContentBlockText,
			sigma.ContentBlockImage,
			sigma.ContentBlockDocument,
		},
	}
	if !multimodal.SupportsInput(sigma.ContentBlockImage) {
		t.Fatal("vision model did not support image input")
	}
	if !multimodal.SupportsImages() {
		t.Fatal("vision model did not report SupportsImages")
	}
	if !multimodal.SupportsInput(sigma.ContentBlockDocument) {
		t.Fatal("multimodal model did not support document input")
	}
	if !multimodal.SupportsDocuments() {
		t.Fatal("multimodal model did not report SupportsDocuments")
	}
	if multimodal.SupportsInput(sigma.ContentBlockToolCall) {
		t.Fatal("tool-call content was supported as model input")
	}
}

func TestCacheRetentionHelpers(t *testing.T) {
	t.Parallel()

	if sigma.CacheRetention("").CacheEnabled() {
		t.Fatal("empty cache retention enabled caching")
	}
	if sigma.CacheRetentionNone.CacheEnabled() {
		t.Fatal("none cache retention enabled caching")
	}
	if !sigma.CacheRetentionShort.CacheEnabled() || !sigma.CacheRetentionShort.CacheShortLived() {
		t.Fatal("short cache retention was not short-lived")
	}
	if !sigma.CacheRetentionLong.CacheEnabled() || !sigma.CacheRetentionLong.CacheLongLived() {
		t.Fatal("long cache retention was not long-lived")
	}
	if !sigma.CacheRetentionEphemeral.CacheShortLived() {
		t.Fatal("ephemeral cache retention was not short-lived")
	}
	if !sigma.CacheRetentionPersistent.CacheLongLived() {
		t.Fatal("persistent cache retention was not long-lived")
	}
}
