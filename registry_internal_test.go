// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"testing"
	"time"
)

func TestRegistryReadsDoNotAcquireWriteLock(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	if err := registry.RegisterModel(Model{
		ID:       "read-lock-model",
		Provider: ProviderCustom,
		API:      APIOpenAICompletions,
	}, WithMetadataOnly()); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	registry.mu.RLock()
	defer registry.mu.RUnlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, ok := registry.Model(ProviderCustom, "read-lock-model"); !ok {
			t.Error("Model did not find registered model")
		}
		if got := len(registry.ListModels()); got != 1 {
			t.Errorf("ListModels count = %d, want 1", got)
		}
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("registry read blocked behind another read lock")
	}
}
