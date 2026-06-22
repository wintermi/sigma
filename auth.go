// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/wintermi/sigma/internal/redact"
)

// CredentialType identifies the kind of authentication material.
type CredentialType string

const (
	// CredentialTypeAPIKey identifies a static API key.
	CredentialTypeAPIKey CredentialType = "api-key"
	// CredentialTypeOAuthToken identifies a bearer token from an OAuth provider.
	CredentialTypeOAuthToken CredentialType = "oauth-token"
	// CredentialTypeCloudCredential identifies cloud provider credential material.
	CredentialTypeCloudCredential CredentialType = "cloud-credential"
)

const (
	// MetadataAPIKeyEnvVar names one API-key environment variable in model metadata.
	MetadataAPIKeyEnvVar = "apiKeyEnvVar"
	// MetadataAPIKeyEnvVars names ordered API-key environment variables in model metadata.
	MetadataAPIKeyEnvVars = "apiKeyEnvVars"
)

const (
	defaultOpenAIAPIKeyEnv       = "OPENAI_API_KEY"
	defaultAzureOpenAIAPIKeyEnv  = "AZURE_OPENAI_API_KEY"
	defaultAnthropicAPIKeyEnv    = "ANTHROPIC_API_KEY"
	defaultGoogleAPIKeyEnv       = "GOOGLE_API_KEY"
	defaultGoogleCloudAPIKeyEnv  = "GOOGLE_CLOUD_API_KEY"
	defaultMistralAPIKeyEnv      = "MISTRAL_API_KEY"
	defaultOpenRouterAPIKeyEnv   = "OPENROUTER_API_KEY"
	defaultXAIAPIKeyEnv          = "XAI_API_KEY"
	defaultCloudflareAPIKeyEnv   = "CLOUDFLARE_API_KEY"
	defaultGroqAPIKeyEnv         = "GROQ_API_KEY"
	defaultTogetherAPIKeyEnv     = "TOGETHER_API_KEY"
	defaultHuggingFaceTokenEnv   = "HF_TOKEN"
	defaultCopilotGitHubTokenEnv = "COPILOT_GITHUB_TOKEN"
	defaultNVIDIAAPIKeyEnv       = "NVIDIA_API_KEY"
	defaultMoonshotAPIKeyEnv     = "MOONSHOT_API_KEY"
	defaultKimiAPIKeyEnv         = "KIMI_API_KEY"
	defaultFireworksAPIKeyEnv    = "FIREWORKS_API_KEY"
	defaultOpenCodeAPIKeyEnv     = "OPENCODE_API_KEY"
)

var defaultProviderEnvNames = map[ProviderID][]string{
	ProviderOpenAI:                {defaultOpenAIAPIKeyEnv},
	ProviderAzureOpenAIResponses:  {defaultAzureOpenAIAPIKeyEnv},
	ProviderAnthropic:             {defaultAnthropicAPIKeyEnv},
	ProviderGoogle:                {defaultGoogleAPIKeyEnv, defaultGoogleCloudAPIKeyEnv},
	ProviderGoogleVertex:          {defaultGoogleCloudAPIKeyEnv, defaultGoogleAPIKeyEnv},
	ProviderGoogleVertexOpenAI:    {defaultGoogleCloudAPIKeyEnv, defaultGoogleAPIKeyEnv},
	ProviderGoogleVertexAnthropic: {defaultGoogleCloudAPIKeyEnv, defaultGoogleAPIKeyEnv},
	ProviderMistral:               {defaultMistralAPIKeyEnv},
	ProviderOpenRouter:            {defaultOpenRouterAPIKeyEnv},
	ProviderDeepSeek:              {"DEEPSEEK_API_KEY"},
	ProviderGroq:                  {defaultGroqAPIKeyEnv},
	ProviderCerebras:              {"CEREBRAS_API_KEY"},
	ProviderXAI:                   {defaultXAIAPIKeyEnv},
	ProviderTogether:              {defaultTogetherAPIKeyEnv},
	ProviderHuggingFace:           {defaultHuggingFaceTokenEnv},
	ProviderCloudflareAIGateway:   {defaultCloudflareAPIKeyEnv},
	ProviderCloudflareWorkersAI:   {defaultCloudflareAPIKeyEnv},
	ProviderGitHubCopilot:         {defaultCopilotGitHubTokenEnv},
	ProviderNVIDIA:                {defaultNVIDIAAPIKeyEnv},
	ProviderZAI:                   {"ZAI_API_KEY"},
	ProviderZAICodingCN:           {"ZAI_CODING_CN_API_KEY"},
	ProviderAntLing:               {"ANT_LING_API_KEY"},
	ProviderMoonshotAI:            {defaultMoonshotAPIKeyEnv},
	ProviderMoonshotAICN:          {defaultMoonshotAPIKeyEnv},
	ProviderMiniMax:               {"MINIMAX_API_KEY"},
	ProviderMiniMaxCN:             {"MINIMAX_CN_API_KEY"},
	ProviderVercelAIGateway:       {"AI_GATEWAY_API_KEY"},
	ProviderOpenCode:              {defaultOpenCodeAPIKeyEnv},
	ProviderOpenCodeGo:            {defaultOpenCodeAPIKeyEnv},
	ProviderFireworks:             {defaultFireworksAPIKeyEnv},
	ProviderFireworksAnthropic:    {defaultFireworksAPIKeyEnv},
	ProviderKimi:                  {defaultKimiAPIKeyEnv},
	ProviderKimiCoding:            {defaultKimiAPIKeyEnv},
	ProviderXiaomi:                {"XIAOMI_API_KEY"},
	ProviderXiaomiTokenPlanCN:     {"XIAOMI_TOKEN_PLAN_CN_API_KEY"},
	ProviderXiaomiTokenPlanAMS:    {"XIAOMI_TOKEN_PLAN_AMS_API_KEY"},
	ProviderXiaomiTokenPlanSGP:    {"XIAOMI_TOKEN_PLAN_SGP_API_KEY"},
}

// Credential carries authentication material for a provider.
type Credential struct {
	Type     CredentialType
	Value    string
	Expiry   time.Time
	Source   string
	Metadata map[string]any
}

// String returns a diagnostic-safe credential description.
func (c Credential) String() string {
	return c.safeString()
}

// Format prevents fmt from printing Credential.Value with struct formatting verbs.
func (c Credential) Format(state fmt.State, verb rune) {
	_, _ = io.WriteString(state, c.safeString())
}

func (c Credential) safeString() string {
	parts := []string{"credential"}
	if c.Type != "" {
		parts = append(parts, "type="+string(c.Type))
	}
	if c.Source != "" {
		parts = append(parts, "source="+redact.Source(c.Source))
	}
	if !c.Expiry.IsZero() {
		parts = append(parts, "expiry="+c.Expiry.Format(time.RFC3339))
	}
	if len(c.Metadata) > 0 {
		parts = append(parts, "metadata="+strings.Join(sortedAnyMapKeys(c.Metadata), ","))
	}
	if c.Value != "" {
		parts = append(parts, "value="+redact.Secret(c.Value))
	}
	return strings.Join(parts, " ")
}

// AuthResolver resolves provider credentials for a request.
type AuthResolver interface {
	Resolve(context.Context, Model, Options) (Credential, error)
}

// AuthResolverFunc adapts a function into an AuthResolver.
type AuthResolverFunc func(context.Context, Model, Options) (Credential, error)

// Resolve calls f.
func (f AuthResolverFunc) Resolve(ctx context.Context, model Model, opts Options) (Credential, error) {
	if f == nil {
		return Credential{}, unavailableCredential(model)
	}
	return f(ctx, model, opts)
}

// OAuthTokenProvider provides OAuth tokens for a provider adapter.
type OAuthTokenProvider interface {
	Token(context.Context, Model, Options) (Credential, error)
}

// OAuthTokenProviderFunc adapts a function into an OAuthTokenProvider.
type OAuthTokenProviderFunc func(context.Context, Model, Options) (Credential, error)

// Token calls f.
func (f OAuthTokenProviderFunc) Token(ctx context.Context, model Model, opts Options) (Credential, error) {
	if f == nil {
		return Credential{}, unavailableCredential(model, "oauth-token-provider")
	}
	return f(ctx, model, opts)
}

// CloudCredentialProvider provides cloud credential material for a provider adapter.
type CloudCredentialProvider interface {
	Credential(context.Context, Model, Options) (Credential, error)
}

// CredentialUnavailableError reports a failed credential lookup without secrets.
type CredentialUnavailableError struct {
	Provider ProviderID
	Model    ModelID
	Sources  []string
}

// Error returns diagnostic-safe source information.
func (e *CredentialUnavailableError) Error() string {
	if e == nil {
		return ""
	}
	message := "credential unavailable"
	if e.Provider != "" || e.Model != "" {
		message += fmt.Sprintf(" for %s/%s", e.Provider, e.Model)
	}
	if len(e.Sources) > 0 {
		message += " after checking " + strings.Join(redactSources(e.Sources), ", ")
	}
	return message
}

// Is supports errors.Is(err, ErrCredentialUnavailable).
func (e *CredentialUnavailableError) Is(target error) bool {
	return target == ErrCredentialUnavailable
}

// EnvironmentAuthResolver resolves static API keys from environment variables.
type EnvironmentAuthResolver struct {
	LookupEnv func(string) (string, bool)
}

// EnvVars returns the ordered environment variable names that would be checked
// for model credentials. Model metadata takes precedence over provider
// defaults. Secret values are not returned.
func (r EnvironmentAuthResolver) EnvVars(model Model) []string {
	return environmentCredentialSources(model)
}

// ConfiguredEnvVars returns the ordered environment variable names that are
// currently set to non-empty values for model credentials. Secret values are
// not returned.
func (r EnvironmentAuthResolver) ConfiguredEnvVars(model Model) []string {
	lookup := r.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	var configured []string
	for _, source := range r.EnvVars(model) {
		value, ok := lookup(source)
		if ok && value != "" {
			configured = append(configured, source)
		}
	}
	return configured
}

// Resolve returns the first non-empty provider API key found in the environment.
func (r EnvironmentAuthResolver) Resolve(_ context.Context, model Model, _ Options) (Credential, error) {
	lookup := r.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	sources := r.EnvVars(model)
	for _, source := range sources {
		value, ok := lookup(source)
		if ok && value != "" {
			return Credential{
				Type:   CredentialTypeAPIKey,
				Value:  value,
				Source: "env:" + source,
			}, nil
		}
	}
	return Credential{}, unavailableCredential(model, prefixSources("env:", sources)...)
}

// ChainAuthResolver resolves credentials through sigma's standard precedence.
type ChainAuthResolver struct {
	Client            AuthResolver
	Environment       AuthResolver
	ProviderCallbacks map[ProviderID]AuthResolver
}

// Resolve checks request overrides, the client resolver, environment, then provider callbacks.
func (r ChainAuthResolver) Resolve(ctx context.Context, model Model, opts Options) (Credential, error) {
	if opts.APIKey != "" {
		return Credential{
			Type:   CredentialTypeAPIKey,
			Value:  opts.APIKey,
			Source: "request:api-key",
		}, nil
	}

	var sources []string
	resolverOptions := opts
	resolverOptions.AuthResolver = nil

	if r.Client != nil {
		credential, err := r.Client.Resolve(ctx, model, resolverOptions)
		if err == nil {
			return credential, nil
		}
		if !errors.Is(err, ErrCredentialUnavailable) {
			return Credential{}, err
		}
		sources = append(sources, credentialErrorSources(err)...)
	}

	environment := r.Environment
	if environment == nil {
		environment = EnvironmentAuthResolver{}
	}
	credential, err := environment.Resolve(ctx, model, resolverOptions)
	if err == nil {
		return credential, nil
	}
	if !errors.Is(err, ErrCredentialUnavailable) {
		return Credential{}, err
	}
	sources = append(sources, credentialErrorSources(err)...)

	if callback := r.ProviderCallbacks[model.Provider]; callback != nil {
		credential, err := callback.Resolve(ctx, model, resolverOptions)
		if err == nil {
			return credential, nil
		}
		if !errors.Is(err, ErrCredentialUnavailable) {
			return Credential{}, err
		}
		sources = append(sources, credentialErrorSources(err)...)
	}

	return Credential{}, unavailableCredential(model, sources...)
}

func unavailableCredential(model Model, sources ...string) error {
	return &CredentialUnavailableError{
		Provider: model.Provider,
		Model:    model.ID,
		Sources:  uniqueStrings(sources),
	}
}

func credentialErrorSources(err error) []string {
	var unavailable *CredentialUnavailableError
	if errors.As(err, &unavailable) {
		return unavailable.Sources
	}
	return nil
}

func environmentCredentialSources(model Model) []string {
	var sources []string
	sources = appendMetadataEnvNames(sources, model.ProviderMetadata[MetadataAPIKeyEnvVar])
	sources = appendMetadataEnvNames(sources, model.ProviderMetadata[MetadataAPIKeyEnvVars])
	sources = append(sources, defaultEnvNames(model.Provider)...)
	return uniqueStrings(sources)
}

func appendMetadataEnvNames(sources []string, value any) []string {
	switch names := value.(type) {
	case string:
		if names != "" {
			sources = append(sources, names)
		}
	case []string:
		sources = append(sources, names...)
	case []any:
		for _, name := range names {
			if text, ok := name.(string); ok && text != "" {
				sources = append(sources, text)
			}
		}
	}
	return sources
}

func defaultEnvNames(provider ProviderID) []string {
	names := defaultProviderEnvNames[provider]
	if len(names) == 0 {
		return nil
	}
	return append([]string(nil), names...)
}

func prefixSources(prefix string, sources []string) []string {
	prefixed := make([]string, 0, len(sources))
	for _, source := range sources {
		prefixed = append(prefixed, prefix+source)
	}
	return prefixed
}

func redactSources(sources []string) []string {
	redacted := make([]string, 0, len(sources))
	for _, source := range sources {
		redacted = append(redacted, redact.Source(source))
	}
	return redacted
}

func sortedAnyMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}
