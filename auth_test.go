// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wintermi/sigma"
)

func TestCredentialFormattingRedactsValue(t *testing.T) {
	t.Parallel()

	credential := sigma.Credential{
		Type:   sigma.CredentialTypeAPIKey,
		Value:  "sk-test-secret",
		Source: "env:OPENAI_API_KEY=sk-test-secret",
		Expiry: time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC),
		Metadata: map[string]any{
			"tenant": "secret-tenant",
		},
	}

	for _, formatted := range []string{
		credential.String(),
		fmt.Sprint(credential),
		fmt.Sprintf("%+v", credential),
		fmt.Sprintf("%#v", credential),
	} {
		if strings.Contains(formatted, "sk-test-secret") {
			t.Fatalf("formatted credential leaked secret: %q", formatted)
		}
		if !strings.Contains(formatted, "[redacted]") {
			t.Fatalf("formatted credential = %q, want redaction marker", formatted)
		}
	}
}

func TestEnvironmentAuthResolverUsesMetadataBeforeDefaultNames(t *testing.T) {
	clearCredentialEnv(t)
	t.Setenv("CUSTOM_OPENAI_KEY", "metadata-secret")
	t.Setenv("OPENAI_API_KEY", "default-secret")

	model := sigma.Model{
		ID:       "custom-openai",
		Provider: sigma.ProviderOpenAI,
		ProviderMetadata: map[string]any{
			sigma.MetadataAPIKeyEnvVar: "CUSTOM_OPENAI_KEY",
		},
	}

	credential, err := (sigma.EnvironmentAuthResolver{}).Resolve(context.Background(), model, sigma.Options{})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got, want := credential.Value, "metadata-secret"; got != want {
		t.Fatalf("credential value = %q, want %q", got, want)
	}
	if got, want := credential.Source, "env:CUSTOM_OPENAI_KEY"; got != want {
		t.Fatalf("credential source = %q, want %q", got, want)
	}
}

func TestEnvironmentAuthResolverEnvVarsUsesMetadataBeforeDefaultNames(t *testing.T) {
	t.Parallel()

	model := sigma.Model{
		ID:       "custom-openai",
		Provider: sigma.ProviderOpenAI,
		ProviderMetadata: map[string]any{
			sigma.MetadataAPIKeyEnvVar:  "CUSTOM_OPENAI_KEY",
			sigma.MetadataAPIKeyEnvVars: []any{"FALLBACK_OPENAI_KEY", "CUSTOM_OPENAI_KEY", "OPENAI_API_KEY"},
		},
	}

	got := (sigma.EnvironmentAuthResolver{}).EnvVars(model)
	want := []string{"CUSTOM_OPENAI_KEY", "FALLBACK_OPENAI_KEY", "OPENAI_API_KEY"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EnvVars() = %#v, want %#v", got, want)
	}
}

func TestEnvironmentAuthResolverConfiguredEnvVarsUsesLookupWithoutValues(t *testing.T) {
	t.Parallel()

	model := sigma.Model{
		ID:       "custom-openai",
		Provider: sigma.ProviderOpenAI,
		ProviderMetadata: map[string]any{
			sigma.MetadataAPIKeyEnvVars: []string{"EMPTY_OPENAI_KEY", "CUSTOM_OPENAI_KEY"},
		},
	}
	resolver := sigma.EnvironmentAuthResolver{
		LookupEnv: func(name string) (string, bool) {
			values := map[string]string{
				"EMPTY_OPENAI_KEY":  "",
				"CUSTOM_OPENAI_KEY": "metadata-secret",
				"OPENAI_API_KEY":    "default-secret",
			}
			value, ok := values[name]
			return value, ok
		},
	}

	got := resolver.ConfiguredEnvVars(model)
	want := []string{"CUSTOM_OPENAI_KEY", "OPENAI_API_KEY"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ConfiguredEnvVars() = %#v, want %#v", got, want)
	}
}

func TestEnvironmentAuthResolverCommonStaticKeys(t *testing.T) {
	tests := []struct {
		name     string
		provider sigma.ProviderID
		env      string
	}{
		{name: "openai", provider: sigma.ProviderOpenAI, env: "OPENAI_API_KEY"},
		{name: "azure openai", provider: sigma.ProviderAzureOpenAIResponses, env: "AZURE_OPENAI_API_KEY"},
		{name: "anthropic", provider: sigma.ProviderAnthropic, env: "ANTHROPIC_API_KEY"},
		{name: "google", provider: sigma.ProviderGoogle, env: "GOOGLE_API_KEY"},
		{name: "google cloud", provider: sigma.ProviderGoogleVertex, env: "GOOGLE_CLOUD_API_KEY"},
		{name: "google vertex openai", provider: sigma.ProviderGoogleVertexOpenAI, env: "GOOGLE_CLOUD_API_KEY"},
		{name: "google vertex anthropic", provider: sigma.ProviderGoogleVertexAnthropic, env: "GOOGLE_CLOUD_API_KEY"},
		{name: "mistral", provider: sigma.ProviderMistral, env: "MISTRAL_API_KEY"},
		{name: "openrouter", provider: sigma.ProviderOpenRouter, env: "OPENROUTER_API_KEY"},
		{name: "deepseek", provider: sigma.ProviderDeepSeek, env: "DEEPSEEK_API_KEY"},
		{name: "groq", provider: sigma.ProviderGroq, env: "GROQ_API_KEY"},
		{name: "cerebras", provider: sigma.ProviderCerebras, env: "CEREBRAS_API_KEY"},
		{name: "xai", provider: sigma.ProviderXAI, env: "XAI_API_KEY"},
		{name: "together", provider: sigma.ProviderTogether, env: "TOGETHER_API_KEY"},
		{name: "cloudflare ai gateway", provider: sigma.ProviderCloudflareAIGateway, env: "CLOUDFLARE_API_KEY"},
		{name: "github copilot", provider: sigma.ProviderGitHubCopilot, env: "COPILOT_GITHUB_TOKEN"},
		{name: "nvidia", provider: sigma.ProviderNVIDIA, env: "NVIDIA_API_KEY"},
		{name: "zai", provider: sigma.ProviderZAI, env: "ZAI_API_KEY"},
		{name: "ant ling", provider: sigma.ProviderAntLing, env: "ANT_LING_API_KEY"},
		{name: "moonshot", provider: sigma.ProviderMoonshotAI, env: "MOONSHOT_API_KEY"},
		{name: "minimax", provider: sigma.ProviderMiniMax, env: "MINIMAX_API_KEY"},
		{name: "vercel ai gateway", provider: sigma.ProviderVercelAIGateway, env: "AI_GATEWAY_API_KEY"},
		{name: "opencode", provider: sigma.ProviderOpenCode, env: "OPENCODE_API_KEY"},
		{name: "opencode go", provider: sigma.ProviderOpenCodeGo, env: "OPENCODE_API_KEY"},
		{name: "fireworks", provider: sigma.ProviderFireworks, env: "FIREWORKS_API_KEY"},
		{name: "fireworks anthropic", provider: sigma.ProviderFireworksAnthropic, env: "FIREWORKS_API_KEY"},
		{name: "kimi", provider: sigma.ProviderKimi, env: "KIMI_API_KEY"},
		{name: "kimi coding", provider: sigma.ProviderKimiCoding, env: "KIMI_API_KEY"},
		{name: "xiaomi", provider: sigma.ProviderXiaomi, env: "XIAOMI_API_KEY"},
		{name: "xiaomi token plan cn", provider: sigma.ProviderXiaomiTokenPlanCN, env: "XIAOMI_TOKEN_PLAN_CN_API_KEY"},
		{name: "xiaomi token plan ams", provider: sigma.ProviderXiaomiTokenPlanAMS, env: "XIAOMI_TOKEN_PLAN_AMS_API_KEY"},
		{name: "xiaomi token plan sgp", provider: sigma.ProviderXiaomiTokenPlanSGP, env: "XIAOMI_TOKEN_PLAN_SGP_API_KEY"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearCredentialEnv(t)
			t.Setenv(tt.env, "env-secret")

			model := sigma.Model{ID: "model", Provider: tt.provider}
			credential, err := (sigma.EnvironmentAuthResolver{}).Resolve(context.Background(), model, sigma.Options{})
			if err != nil {
				t.Fatalf("Resolve returned error: %v", err)
			}
			if got, want := credential.Value, "env-secret"; got != want {
				t.Fatalf("credential value = %q, want %q", got, want)
			}
			if got, want := credential.Source, "env:"+tt.env; got != want {
				t.Fatalf("credential source = %q, want %q", got, want)
			}
		})
	}
}

func TestEnvironmentAuthResolverCopilotIgnoresGenericGitHubTokens(t *testing.T) {
	t.Parallel()

	resolver := sigma.EnvironmentAuthResolver{
		LookupEnv: func(name string) (string, bool) {
			values := map[string]string{
				"GH_TOKEN":     "gh-token",
				"GITHUB_TOKEN": "github-token",
			}
			value, ok := values[name]
			return value, ok
		},
	}
	model := sigma.Model{ID: "copilot-test", Provider: sigma.ProviderGitHubCopilot}

	if got := resolver.ConfiguredEnvVars(model); len(got) != 0 {
		t.Fatalf("ConfiguredEnvVars() = %#v, want no configured Copilot env vars", got)
	}
	_, err := resolver.Resolve(context.Background(), model, sigma.Options{})
	if err == nil {
		t.Fatal("Resolve returned nil error")
	}
	if !errors.Is(err, sigma.ErrCredentialUnavailable) {
		t.Fatalf("Resolve error = %v, want ErrCredentialUnavailable", err)
	}
}

func TestChainAuthResolverPrecedence(t *testing.T) {
	clearCredentialEnv(t)
	t.Setenv("OPENAI_API_KEY", "env-secret")

	model := sigma.Model{ID: "gpt-test", Provider: sigma.ProviderOpenAI}
	clientResolver := sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{
			Type:   sigma.CredentialTypeAPIKey,
			Value:  "client-secret",
			Source: "client:test",
		}, nil
	})
	callbackResolver := sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{
			Type:   sigma.CredentialTypeOAuthToken,
			Value:  "callback-secret",
			Source: "provider:test",
		}, nil
	})
	resolver := sigma.ChainAuthResolver{
		Client: clientResolver,
		ProviderCallbacks: map[sigma.ProviderID]sigma.AuthResolver{
			sigma.ProviderOpenAI: callbackResolver,
		},
	}

	credential, err := resolver.Resolve(context.Background(), model, sigma.Options{APIKey: "request-secret"})
	if err != nil {
		t.Fatalf("Resolve(request) returned error: %v", err)
	}
	if got, want := credential.Value, "request-secret"; got != want {
		t.Fatalf("request credential = %q, want %q", got, want)
	}

	credential, err = resolver.Resolve(context.Background(), model, sigma.Options{})
	if err != nil {
		t.Fatalf("Resolve(callback) returned error: %v", err)
	}
	if got, want := credential.Value, "callback-secret"; got != want {
		t.Fatalf("callback credential = %q, want %q", got, want)
	}
}

func TestChainAuthResolverFallsBackToEnvironmentAndCallbacks(t *testing.T) {
	clearCredentialEnv(t)
	t.Setenv("OPENAI_API_KEY", "env-secret")

	model := sigma.Model{ID: "gpt-test", Provider: sigma.ProviderOpenAI}
	missingClient := sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{}, sigma.ErrCredentialUnavailable
	})

	credential, err := (sigma.ChainAuthResolver{Client: missingClient}).Resolve(context.Background(), model, sigma.Options{})
	if err != nil {
		t.Fatalf("Resolve(environment) returned error: %v", err)
	}
	if got, want := credential.Value, "env-secret"; got != want {
		t.Fatalf("environment credential = %q, want %q", got, want)
	}

	callback := sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{
			Type:   sigma.CredentialTypeOAuthToken,
			Value:  "callback-secret",
			Source: "provider:test",
		}, nil
	})
	credential, err = (sigma.ChainAuthResolver{
		Client: missingClient,
		ProviderCallbacks: map[sigma.ProviderID]sigma.AuthResolver{
			sigma.ProviderOpenAI: callback,
		},
	}).Resolve(context.Background(), model, sigma.Options{})
	if err != nil {
		t.Fatalf("Resolve(callback) returned error: %v", err)
	}
	if got, want := credential.Value, "callback-secret"; got != want {
		t.Fatalf("callback credential = %q, want %q", got, want)
	}
}

func TestCredentialUnavailableErrorIsTypedAndRedacted(t *testing.T) {
	clearCredentialEnv(t)
	t.Setenv("OPENAI_API_KEY", "env-secret")

	model := sigma.Model{ID: "claude-test", Provider: sigma.ProviderAnthropic}
	_, err := (sigma.ChainAuthResolver{}).Resolve(context.Background(), model, sigma.Options{})
	if err == nil {
		t.Fatal("Resolve returned nil error")
	}
	if !errors.Is(err, sigma.ErrCredentialUnavailable) {
		t.Fatalf("error %T does not match ErrCredentialUnavailable", err)
	}
	if strings.Contains(err.Error(), "env-secret") {
		t.Fatalf("error leaked secret: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Fatalf("error = %q, want checked source", err.Error())
	}
}

func TestClientAuthResolverCanBeReplacedInTests(t *testing.T) {
	t.Parallel()

	clientResolver := sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{
			Type:   sigma.CredentialTypeAPIKey,
			Value:  "fake-secret",
			Source: "test",
		}, nil
	})

	client, provider, model := newOptionsTestClient(t, sigma.WithAuthResolver(clientResolver))
	if _, err := client.Complete(context.Background(), model, sigma.Request{}); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	credential, err := provider.opts.AuthResolver.Resolve(context.Background(), model, provider.opts)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got, want := credential.Value, "fake-secret"; got != want {
		t.Fatalf("credential value = %q, want %q", got, want)
	}
}

func clearCredentialEnv(t *testing.T) {
	t.Helper()

	for _, name := range []string{
		"OPENAI_API_KEY",
		"ANTHROPIC_API_KEY",
		"GOOGLE_API_KEY",
		"GOOGLE_CLOUD_API_KEY",
		"MISTRAL_API_KEY",
		"OPENROUTER_API_KEY",
		"CUSTOM_OPENAI_KEY",
		"COPILOT_GITHUB_TOKEN",
		"GH_TOKEN",
		"GITHUB_TOKEN",
		"DEEPSEEK_API_KEY",
		"GROQ_API_KEY",
		"CEREBRAS_API_KEY",
		"TOGETHER_API_KEY",
		"FIREWORKS_API_KEY",
		"OPENCODE_API_KEY",
		"XIAOMI_API_KEY",
	} {
		t.Setenv(name, "")
	}
}
