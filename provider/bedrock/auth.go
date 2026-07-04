// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package bedrock

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	if credentials, ok := envAWSCredentials(); ok {
		return credentials, nil
	}
	if credentials, ok := profileAWSCredentials(); ok {
		return credentials, nil
	}
	if credentials, ok := ecsCredentials(ctx); ok {
		return credentials, nil
	}
	if credentials, ok := webIdentityCredentials(ctx); ok {
		return credentials, nil
	}
	if credentials, ok := imdsCredentials(ctx); ok {
		return credentials, nil
	}
	return CredentialInfo{}, credentialError("bedrock converse stream: retrieve AWS credentials", sigma.ErrCredentialUnavailable, bedrockConfig, []string{
		"env:AWS_BEARER_TOKEN_BEDROCK",
		"env:AWS_ACCESS_KEY_ID",
		"env:AWS_SECRET_ACCESS_KEY",
		"env:AWS_PROFILE",
		"env:AWS_CONTAINER_CREDENTIALS_RELATIVE_URI",
		"env:AWS_CONTAINER_CREDENTIALS_FULL_URI",
		"env:AWS_WEB_IDENTITY_TOKEN_FILE",
		"imds",
	})
}

func envAWSCredentials() (CredentialInfo, bool) {
	accessKeyID := os.Getenv("AWS_ACCESS_KEY_ID")
	secretAccessKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if accessKeyID == "" || secretAccessKey == "" {
		return CredentialInfo{}, false
	}
	return CredentialInfo{
		Source:          CredentialSourceDefaultChain,
		AccessKeyID:     accessKeyID,
		SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
		SecretAccessKey: secretAccessKey,
	}, true
}

func profileAWSCredentials() (CredentialInfo, bool) {
	profile := os.Getenv("AWS_PROFILE")
	if profile == "" {
		profile = "default"
	}
	for _, file := range awsCredentialFiles() {
		values := readAWSProfile(file, profile)
		if len(values) == 0 && profile != "default" {
			values = readAWSProfile(file, "profile "+profile)
		}
		accessKeyID := values["aws_access_key_id"]
		secretAccessKey := values["aws_secret_access_key"]
		if accessKeyID == "" || secretAccessKey == "" {
			continue
		}
		return CredentialInfo{
			Source:          CredentialSourceDefaultChain,
			AccessKeyID:     accessKeyID,
			SecretAccessKey: secretAccessKey,
			SessionToken:    values["aws_session_token"],
		}, true
	}
	return CredentialInfo{}, false
}

func awsCredentialFiles() []string {
	var files []string
	if file := os.Getenv("AWS_SHARED_CREDENTIALS_FILE"); file != "" {
		files = append(files, file)
	}
	if file := os.Getenv("AWS_CONFIG_FILE"); file != "" {
		files = append(files, file)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		files = append(files,
			filepath.Join(home, ".aws", "credentials"),
			filepath.Join(home, ".aws", "config"),
		)
	}
	return files
}

func readAWSProfile(file string, profile string) map[string]string {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	values := make(map[string]string)
	var section string
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.Contains(line, "]") {
			section = strings.TrimSpace(line[1:strings.Index(line, "]")])
			continue
		}
		if section != profile {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return values
}

type awsCredentialPayload struct {
	AccessKeyID     string `json:"AccessKeyId"`
	SecretAccessKey string `json:"SecretAccessKey"`
	Token           string `json:"Token"`
}

func ecsCredentials(ctx context.Context) (CredentialInfo, bool) {
	endpoint := os.Getenv("AWS_CONTAINER_CREDENTIALS_FULL_URI")
	if endpoint == "" {
		if relative := os.Getenv("AWS_CONTAINER_CREDENTIALS_RELATIVE_URI"); relative != "" {
			endpoint = "http://169.254.170.2" + relative
		}
	}
	if endpoint == "" {
		return CredentialInfo{}, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil) // #nosec G704 -- ECS credential endpoints are controlled by AWS container credential env vars.
	if err != nil {
		return CredentialInfo{}, false
	}
	if tokenFile := os.Getenv("AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE"); tokenFile != "" {
		if data, err := os.ReadFile(tokenFile); err == nil { // #nosec G703 -- AWS defines this credential-token file path via environment.
			req.Header.Set("Authorization", strings.TrimSpace(string(data)))
		}
	} else if token := os.Getenv("AWS_CONTAINER_AUTHORIZATION_TOKEN"); token != "" {
		req.Header.Set("Authorization", token)
	}
	return awsCredentialsFromRequest(req)
}

func webIdentityCredentials(ctx context.Context) (CredentialInfo, bool) {
	tokenFile := os.Getenv("AWS_WEB_IDENTITY_TOKEN_FILE")
	roleARN := os.Getenv("AWS_ROLE_ARN")
	if tokenFile == "" || roleARN == "" {
		return CredentialInfo{}, false
	}
	token, err := os.ReadFile(tokenFile) // #nosec G703 -- AWS defines this web-identity token file path via environment.
	if err != nil {
		return CredentialInfo{}, false
	}
	endpoint := os.Getenv("AWS_STS_ENDPOINT")
	if endpoint == "" {
		endpoint = "https://sts.amazonaws.com"
	}
	form := url.Values{}
	form.Set("Action", "AssumeRoleWithWebIdentity")
	form.Set("Version", "2011-06-15")
	form.Set("RoleArn", roleARN)
	form.Set("RoleSessionName", firstNonEmpty(os.Getenv("AWS_ROLE_SESSION_NAME"), "sigma-bedrock"))
	form.Set("WebIdentityToken", strings.TrimSpace(string(token)))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode())) // #nosec G704 -- STS endpoint override follows AWS-compatible environment behavior.
	if err != nil {
		return CredentialInfo{}, false
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := awsCredentialHTTPClient().Do(req) // #nosec G704 -- STS credential exchange endpoint is AWS-compatible configuration.
	if err != nil {
		return CredentialInfo{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return CredentialInfo{}, false
	}
	var decoded struct {
		Result struct {
			Credentials struct {
				AccessKeyID     string `xml:"AccessKeyId"`
				SecretAccessKey string `xml:"SecretAccessKey"`
				SessionToken    string `xml:"SessionToken"`
			} `xml:"Credentials"`
		} `xml:"AssumeRoleWithWebIdentityResult"`
	}
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&decoded); err != nil {
		return CredentialInfo{}, false
	}
	credentials := decoded.Result.Credentials
	if credentials.AccessKeyID == "" || credentials.SecretAccessKey == "" {
		return CredentialInfo{}, false
	}
	return CredentialInfo{
		Source:          CredentialSourceDefaultChain,
		AccessKeyID:     credentials.AccessKeyID,
		SecretAccessKey: credentials.SecretAccessKey,
		SessionToken:    credentials.SessionToken,
	}, true
}

func imdsCredentials(ctx context.Context) (CredentialInfo, bool) {
	if strings.EqualFold(os.Getenv("AWS_EC2_METADATA_DISABLED"), "true") {
		return CredentialInfo{}, false
	}
	endpoint := strings.TrimRight(os.Getenv("AWS_EC2_METADATA_SERVICE_ENDPOINT"), "/")
	if endpoint == "" {
		endpoint = "http://169.254.169.254"
	}
	token := imdsToken(ctx, endpoint)
	roleReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/latest/meta-data/iam/security-credentials/", nil) // #nosec G704 -- IMDS endpoint is the AWS metadata service or its documented endpoint override.
	if err != nil {
		return CredentialInfo{}, false
	}
	if token != "" {
		roleReq.Header.Set("X-aws-ec2-metadata-token", token)
	}
	resp, err := awsCredentialHTTPClient().Do(roleReq) // #nosec G704 -- IMDS endpoint is the AWS metadata service or its documented endpoint override.
	if err != nil {
		return CredentialInfo{}, false
	}
	roleData, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()
	if err != nil || resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return CredentialInfo{}, false
	}
	role := strings.TrimSpace(strings.Split(string(roleData), "\n")[0])
	if role == "" {
		return CredentialInfo{}, false
	}
	credReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/latest/meta-data/iam/security-credentials/"+url.PathEscape(role), nil) // #nosec G704 -- IMDS endpoint is the AWS metadata service or its documented endpoint override.
	if err != nil {
		return CredentialInfo{}, false
	}
	if token != "" {
		credReq.Header.Set("X-aws-ec2-metadata-token", token)
	}
	return awsCredentialsFromRequest(credReq)
}

func imdsToken(ctx context.Context, endpoint string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint+"/latest/api/token", nil) // #nosec G704 -- IMDS endpoint is the AWS metadata service or its documented endpoint override.
	if err != nil {
		return ""
	}
	req.Header.Set("X-aws-ec2-metadata-token-ttl-seconds", "21600")
	resp, err := awsCredentialHTTPClient().Do(req) // #nosec G704 -- IMDS endpoint is the AWS metadata service or its documented endpoint override.
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func awsCredentialsFromRequest(req *http.Request) (CredentialInfo, bool) {
	resp, err := awsCredentialHTTPClient().Do(req) // #nosec G704 -- credential endpoint request is constructed by default-chain providers above.
	if err != nil {
		return CredentialInfo{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return CredentialInfo{}, false
	}
	var decoded awsCredentialPayload
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&decoded); err != nil {
		return CredentialInfo{}, false
	}
	if decoded.AccessKeyID == "" || decoded.SecretAccessKey == "" {
		return CredentialInfo{}, false
	}
	return CredentialInfo{
		Source:          CredentialSourceDefaultChain,
		AccessKeyID:     decoded.AccessKeyID,
		SecretAccessKey: decoded.SecretAccessKey,
		SessionToken:    decoded.Token,
	}, true
}

func awsCredentialHTTPClient() *http.Client {
	return &http.Client{Timeout: time.Second}
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
