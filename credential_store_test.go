package sigma_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wintermi/sigma"
)

func TestInMemoryCredentialStoreCopiesAndDeletesCredentials(t *testing.T) {
	t.Parallel()

	store := sigma.NewInMemoryCredentialStore()
	_, ok, err := store.ModifyCredential(context.Background(), sigma.ProviderOpenAI, func(sigma.StoredCredential, bool) (sigma.StoredCredential, bool, error) {
		return sigma.StoredCredential{
			Type:        sigma.CredentialTypeAPIKey,
			Value:       "stored-secret",
			ProviderEnv: map[string]string{"ACCOUNT": "acct"},
			Metadata:    map[string]any{"source": "test"},
		}, true, nil
	})
	if err != nil {
		t.Fatalf("ModifyCredential returned error: %v", err)
	}
	if !ok {
		t.Fatal("ModifyCredential ok = false, want true")
	}

	credential, ok, err := store.ReadCredential(context.Background(), sigma.ProviderOpenAI)
	if err != nil {
		t.Fatalf("ReadCredential returned error: %v", err)
	}
	if !ok {
		t.Fatal("ReadCredential ok = false, want true")
	}
	credential.ProviderEnv["ACCOUNT"] = "mutated"
	credential.Metadata["source"] = "mutated"

	credential, ok, err = store.ReadCredential(context.Background(), sigma.ProviderOpenAI)
	if err != nil {
		t.Fatalf("ReadCredential returned error: %v", err)
	}
	if !ok || credential.ProviderEnv["ACCOUNT"] != "acct" || credential.Metadata["source"] != "test" {
		t.Fatalf("stored credential was mutated through copied read: %#v", credential)
	}

	credential, ok, err = store.ModifyCredential(context.Background(), sigma.ProviderOpenAI, func(sigma.StoredCredential, bool) (sigma.StoredCredential, bool, error) {
		return sigma.StoredCredential{}, false, nil
	})
	if err != nil {
		t.Fatalf("ModifyCredential preserve returned error: %v", err)
	}
	if !ok || credential.Value != "stored-secret" {
		t.Fatalf("preserved credential = %#v, %v; want existing", credential, ok)
	}

	if err := store.DeleteCredential(context.Background(), sigma.ProviderOpenAI); err != nil {
		t.Fatalf("DeleteCredential returned error: %v", err)
	}
	_, ok, err = store.ReadCredential(context.Background(), sigma.ProviderOpenAI)
	if err != nil {
		t.Fatalf("ReadCredential after delete returned error: %v", err)
	}
	if ok {
		t.Fatal("ReadCredential after delete ok = true, want false")
	}
}

func TestInMemoryCredentialStoreSerializesModifyPerProvider(t *testing.T) {
	t.Parallel()

	store := sigma.NewInMemoryCredentialStore()
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := store.ModifyCredential(context.Background(), sigma.ProviderOpenAI, func(current sigma.StoredCredential, ok bool) (sigma.StoredCredential, bool, error) {
				count := 0
				if ok {
					count = current.Metadata["count"].(int)
				}
				time.Sleep(time.Millisecond)
				return sigma.StoredCredential{
					Type:     sigma.CredentialTypeAPIKey,
					Value:    "stored-secret",
					Metadata: map[string]any{"count": count + 1},
				}, true, nil
			})
			if err != nil {
				t.Errorf("ModifyCredential returned error: %v", err)
			}
		}()
	}
	wg.Wait()

	credential, ok, err := store.ReadCredential(context.Background(), sigma.ProviderOpenAI)
	if err != nil {
		t.Fatalf("ReadCredential returned error: %v", err)
	}
	if !ok || credential.Metadata["count"] != 16 {
		t.Fatalf("credential count = %#v, %v; want 16", credential.Metadata["count"], ok)
	}
}

func TestStoredCredentialAuthResolverUsesStoreBeforeEnvironment(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "env-secret")

	store := sigma.NewInMemoryCredentialStore()
	_, _, err := store.ModifyCredential(context.Background(), sigma.ProviderOpenAI, func(sigma.StoredCredential, bool) (sigma.StoredCredential, bool, error) {
		return sigma.StoredCredential{Type: sigma.CredentialTypeAPIKey, Value: "stored-secret"}, true, nil
	})
	if err != nil {
		t.Fatalf("ModifyCredential returned error: %v", err)
	}
	resolver := sigma.ChainAuthResolver{
		Client: sigma.StoredCredentialAuthResolver{Store: store, Registry: sigma.DefaultRegistry()},
	}

	credential, err := resolver.Resolve(context.Background(), sigma.Model{Provider: sigma.ProviderOpenAI, ID: "gpt-test"}, sigma.Options{})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got, want := credential.Value, "stored-secret"; got != want {
		t.Fatalf("credential value = %q, want %q", got, want)
	}

	credential, err = resolver.Resolve(context.Background(), sigma.Model{Provider: sigma.ProviderOpenAI, ID: "gpt-test"}, sigma.Options{APIKey: "request-secret"})
	if err != nil {
		t.Fatalf("Resolve request override returned error: %v", err)
	}
	if got, want := credential.Value, "request-secret"; got != want {
		t.Fatalf("request credential value = %q, want %q", got, want)
	}
}

func TestStoredCredentialAuthResolverStoredMismatchBlocksEnvironmentFallback(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "env-secret")

	store := sigma.NewInMemoryCredentialStore()
	_, _, err := store.ModifyCredential(context.Background(), sigma.ProviderOpenAI, func(sigma.StoredCredential, bool) (sigma.StoredCredential, bool, error) {
		return sigma.StoredCredential{Type: sigma.CredentialTypeOAuthToken, Value: "oauth-token"}, true, nil
	})
	if err != nil {
		t.Fatalf("ModifyCredential returned error: %v", err)
	}

	_, err = (sigma.ChainAuthResolver{
		Client: sigma.StoredCredentialAuthResolver{Store: store, Registry: sigma.DefaultRegistry()},
	}).Resolve(context.Background(), sigma.Model{Provider: sigma.ProviderOpenAI, ID: "gpt-test"}, sigma.Options{})
	if err == nil {
		t.Fatal("Resolve returned nil error")
	}
	if errors.Is(err, sigma.ErrCredentialUnavailable) {
		t.Fatalf("Resolve error = %v, want non-fallback error", err)
	}
	if strings.Contains(err.Error(), "env-secret") {
		t.Fatalf("Resolve error leaked secret: %v", err)
	}
}

func TestStoredCredentialAuthResolverRefreshesOAuthOnce(t *testing.T) {
	t.Parallel()

	store := sigma.NewInMemoryCredentialStore()
	_, _, err := store.ModifyCredential(context.Background(), sigma.ProviderAnthropic, func(sigma.StoredCredential, bool) (sigma.StoredCredential, bool, error) {
		return sigma.StoredCredential{
			Type:         sigma.CredentialTypeOAuthToken,
			Value:        "old-token",
			RefreshToken: "refresh-token",
			Expiry:       time.Unix(1, 0),
		}, true, nil
	})
	if err != nil {
		t.Fatalf("ModifyCredential returned error: %v", err)
	}

	var mu sync.Mutex
	refreshes := 0
	registry := sigma.NewRegistry()
	if err := sigma.RegisterProviderAuth(registry, sigma.ProviderAnthropic, sigma.ProviderAuth{
		OAuth: &sigma.OAuthAuth{
			Refresh: func(context.Context, sigma.StoredCredential) (sigma.StoredCredential, error) {
				mu.Lock()
				defer mu.Unlock()
				refreshes++
				time.Sleep(time.Millisecond)
				return sigma.StoredCredential{
					Type:         sigma.CredentialTypeOAuthToken,
					Value:        "new-token",
					RefreshToken: "next-refresh",
					Expiry:       time.Now().Add(time.Hour),
				}, nil
			},
			Credential: func(_ context.Context, _ sigma.Model, _ sigma.Options, stored sigma.StoredCredential) (sigma.Credential, error) {
				return sigma.Credential{Type: sigma.CredentialTypeOAuthToken, Value: stored.Value, Source: "test-oauth"}, nil
			},
		},
	}); err != nil {
		t.Fatalf("RegisterProviderAuth returned error: %v", err)
	}
	resolver := sigma.StoredCredentialAuthResolver{Store: store, Registry: registry}
	model := sigma.Model{Provider: sigma.ProviderAnthropic, ID: "claude-test"}

	var wg sync.WaitGroup
	values := make(chan string, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			credential, err := resolver.Resolve(context.Background(), model, sigma.Options{})
			if err != nil {
				t.Errorf("Resolve returned error: %v", err)
				return
			}
			values <- credential.Value
		}()
	}
	wg.Wait()
	close(values)

	for value := range values {
		if value != "new-token" {
			t.Fatalf("credential value = %q, want new-token", value)
		}
	}
	if refreshes != 1 {
		t.Fatalf("refreshes = %d, want 1", refreshes)
	}
	stored, ok, err := store.ReadCredential(context.Background(), sigma.ProviderAnthropic)
	if err != nil {
		t.Fatalf("ReadCredential returned error: %v", err)
	}
	if !ok || stored.Value != "new-token" || stored.RefreshToken != "next-refresh" {
		t.Fatalf("stored credential = %#v, %v; want refreshed", stored, ok)
	}
}

func TestStoredCredentialAuthResolverRefreshFailurePreservesCredential(t *testing.T) {
	t.Parallel()

	store := sigma.NewInMemoryCredentialStore()
	_, _, err := store.ModifyCredential(context.Background(), sigma.ProviderAnthropic, func(sigma.StoredCredential, bool) (sigma.StoredCredential, bool, error) {
		return sigma.StoredCredential{
			Type:         sigma.CredentialTypeOAuthToken,
			Value:        "old-token",
			RefreshToken: "refresh-token",
			Expiry:       time.Unix(1, 0),
		}, true, nil
	})
	if err != nil {
		t.Fatalf("ModifyCredential returned error: %v", err)
	}
	registry := sigma.NewRegistry()
	if err := sigma.RegisterProviderAuth(registry, sigma.ProviderAnthropic, sigma.ProviderAuth{
		OAuth: &sigma.OAuthAuth{
			Refresh: func(context.Context, sigma.StoredCredential) (sigma.StoredCredential, error) {
				return sigma.StoredCredential{}, errors.New("invalid_grant")
			},
			Credential: func(_ context.Context, _ sigma.Model, _ sigma.Options, stored sigma.StoredCredential) (sigma.Credential, error) {
				return sigma.Credential{Type: sigma.CredentialTypeOAuthToken, Value: stored.Value}, nil
			},
		},
	}); err != nil {
		t.Fatalf("RegisterProviderAuth returned error: %v", err)
	}

	_, err = (sigma.StoredCredentialAuthResolver{Store: store, Registry: registry}).Resolve(context.Background(), sigma.Model{Provider: sigma.ProviderAnthropic, ID: "claude-test"}, sigma.Options{})
	if err == nil {
		t.Fatal("Resolve returned nil error")
	}
	stored, ok, readErr := store.ReadCredential(context.Background(), sigma.ProviderAnthropic)
	if readErr != nil {
		t.Fatalf("ReadCredential returned error: %v", readErr)
	}
	if !ok || stored.Value != "old-token" || stored.RefreshToken != "refresh-token" {
		t.Fatalf("stored credential = %#v, %v; want old credential preserved", stored, ok)
	}
}
