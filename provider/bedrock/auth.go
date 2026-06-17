// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package bedrock

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/wintermi/sigma"
)

// CredentialSource identifies how the Bedrock provider should find AWS credentials.
type CredentialSource string

const (
	// CredentialSourceAuto checks sigma's auth resolver first, then the AWS default chain.
	CredentialSourceAuto CredentialSource = ""
	// CredentialSourceDefaultChain uses the stdlib environment credential chain.
	CredentialSourceDefaultChain CredentialSource = "default-chain"
	// CredentialSourceAuthResolver uses sigma.Options.AuthResolver only.
	CredentialSourceAuthResolver CredentialSource = "auth-resolver"
	// CredentialSourceBearerToken uses an explicit request-scoped bearer token.
	CredentialSourceBearerToken CredentialSource = "bearer-token"
	// CredentialSourceStaticCredentials uses request-scoped static AWS credentials.
	CredentialSourceStaticCredentials CredentialSource = "static-credentials"
)

// CredentialInfo is diagnostic-safe AWS credential metadata plus optional static
// credential fields for the HTTP transport.
type CredentialInfo struct {
	Source          CredentialSource
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	BearerToken     string
}

// String returns a redacted credential description.
func (c CredentialInfo) String() string {
	source := string(c.Source)
	if source == "" {
		source = string(CredentialSourceAuto)
	}
	if c.AccessKeyID == "" {
		return "bedrock credential source=" + source
	}
	return "bedrock credential source=" + source + " access_key_id=" + redactAccessKey(c.AccessKeyID)
}

// Format prevents fmt from printing secrets with struct formatting verbs.
func (c CredentialInfo) Format(state fmt.State, verb rune) {
	_, _ = io.WriteString(state, c.String())
}

// CredentialDetector detects AWS credentials without exposing AWS SDK types in
// the provider's fakeable client seam.
type CredentialDetector interface {
	Detect(context.Context, sigma.Model, sigma.Options, Config) (CredentialInfo, error)
}

// DefaultCredentialDetector uses sigma auth callbacks and a stdlib environment
// credential chain.
type DefaultCredentialDetector struct{}

// Detect resolves Bedrock credentials according to config.
func (DefaultCredentialDetector) Detect(ctx context.Context, model sigma.Model, opts sigma.Options, bedrockConfig Config) (CredentialInfo, error) {
	if opts.BedrockOptions != nil && opts.BedrockOptions.BearerToken != "" {
		return CredentialInfo{
			Source:      CredentialSourceBearerToken,
			BearerToken: opts.BedrockOptions.BearerToken,
		}, nil
	}
	source := bedrockConfig.CredentialSource
	switch source {
	case CredentialSourceAuthResolver:
		return authResolverCredential(ctx, model, opts)
	case CredentialSourceDefaultChain:
		return defaultChainCredential(ctx, model, opts, bedrockConfig)
	case CredentialSourceAuto:
		credential, err := authResolverCredential(ctx, model, opts)
		if err == nil {
			return credential, nil
		}
		if !errors.Is(err, sigma.ErrCredentialUnavailable) {
			return CredentialInfo{}, err
		}
		return defaultChainCredential(ctx, model, opts, bedrockConfig)
	default:
		return CredentialInfo{}, credentialError("bedrock converse stream: unsupported credential source", nil, bedrockConfig, []string{string(source)})
	}
}

func authResolverCredential(ctx context.Context, model sigma.Model, opts sigma.Options) (CredentialInfo, error) {
	if opts.AuthResolver == nil {
		return CredentialInfo{}, credentialError("bedrock converse stream: auth resolver is required", sigma.ErrCredentialUnavailable, Config{}, []string{"auth-resolver"})
	}
	credential, err := opts.AuthResolver.Resolve(ctx, model, opts)
	if err != nil {
		return CredentialInfo{}, err
	}
	info := CredentialInfo{
		Source:       CredentialSourceAuthResolver,
		SessionToken: stringMetadata(credential.Metadata, "sessionToken"),
		BearerToken:  stringMetadata(credential.Metadata, "bearerToken"),
	}
	if credential.Source != "" {
		info.Source = CredentialSource(credential.Source)
	}
	info.AccessKeyID = firstNonEmpty(
		stringMetadata(credential.Metadata, "accessKeyID"),
		stringMetadata(credential.Metadata, "access_key_id"),
	)
	info.SecretAccessKey = firstNonEmpty(
		stringMetadata(credential.Metadata, "secretAccessKey"),
		stringMetadata(credential.Metadata, "secret_access_key"),
		credential.Value,
	)
	if credential.Type == sigma.CredentialTypeOAuthToken && info.BearerToken == "" {
		info.BearerToken = credential.Value
	}
	if info.BearerToken == "" && (info.AccessKeyID == "" || info.SecretAccessKey == "") {
		return CredentialInfo{}, credentialError("bedrock converse stream: auth resolver returned incomplete AWS credentials", sigma.ErrCredentialUnavailable, Config{}, []string{"auth-resolver"})
	}
	return info, nil
}

func defaultChainCredential(ctx context.Context, model sigma.Model, opts sigma.Options, bedrockConfig Config) (CredentialInfo, error) {
	_ = ctx
	if bedrockConfig.Region == "" {
		return CredentialInfo{}, credentialError("bedrock converse stream: region is required", nil, bedrockConfig, []string{"region"})
	}
	if credentials, ok := requestStaticCredentials(opts, model.Provider); ok {
		if credentials.AccessKeyID == "" || credentials.SecretAccessKey == "" {
			return CredentialInfo{}, credentialError("bedrock converse stream: request static credentials are incomplete", sigma.ErrCredentialUnavailable, bedrockConfig, []string{"request:static-credentials"})
		}
		return CredentialInfo{
			Source:          CredentialSourceStaticCredentials,
			AccessKeyID:     credentials.AccessKeyID,
			SessionToken:    credentials.SessionToken,
			SecretAccessKey: credentials.SecretAccessKey,
		}, nil
	}
	if bearer := os.Getenv("AWS_BEARER_TOKEN_BEDROCK"); bearer != "" {
		return CredentialInfo{
			Source:      CredentialSourceDefaultChain,
			BearerToken: bearer,
		}, nil
	}
	accessKeyID := os.Getenv("AWS_ACCESS_KEY_ID")
	secretAccessKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if accessKeyID == "" || secretAccessKey == "" {
		return CredentialInfo{}, credentialError("bedrock converse stream: retrieve AWS credentials", sigma.ErrCredentialUnavailable, bedrockConfig, []string{"env:AWS_BEARER_TOKEN_BEDROCK", "env:AWS_ACCESS_KEY_ID", "env:AWS_SECRET_ACCESS_KEY"})
	}
	return CredentialInfo{
		Source:          CredentialSourceDefaultChain,
		AccessKeyID:     accessKeyID,
		SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
		SecretAccessKey: secretAccessKey,
	}, nil
}

func requestStaticCredentials(opts sigma.Options, provider sigma.ProviderID) (StaticCredentials, bool) {
	options := providerOptions(opts, provider)
	value, ok := options[providerOptionRequestStaticCredentials]
	if !ok {
		return StaticCredentials{}, false
	}
	credentials, ok := value.(StaticCredentials)
	if ok {
		return credentials, true
	}
	if credentials, ok := value.(*StaticCredentials); ok && credentials != nil {
		return *credentials, true
	}
	return StaticCredentials{}, false
}

func credentialError(message string, err error, bedrockConfig Config, sources []string) error {
	if len(sources) > 0 || err == nil || errors.Is(err, sigma.ErrCredentialUnavailable) {
		return &sigma.CredentialUnavailableError{
			Provider: sigma.ProviderAmazonBedrock,
			Sources:  appendCredentialSources(sources, bedrockConfig),
		}
	}
	return &sigma.Error{
		Code:    sigma.ErrorProviderResponse,
		Message: message,
		Err:     err,
	}
}

func providerErrorMetadata(err error) (int, string) {
	var providerErr *sigma.ProviderError
	if errors.As(err, &providerErr) {
		return providerErr.StatusCode, providerErr.RequestID
	}
	return 0, ""
}

func appendCredentialSources(sources []string, bedrockConfig Config) []string {
	out := append([]string(nil), sources...)
	if bedrockConfig.Region != "" {
		out = append(out, "region:"+bedrockConfig.Region)
	}
	return out
}

func stringMetadata(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value, _ := metadata[key].(string)
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func redactAccessKey(value string) string {
	if len(value) <= 4 {
		return "****"
	}
	return value[:2] + "..." + value[len(value)-2:]
}
