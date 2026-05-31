// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import "github.com/wintermi/sigma/internal/redact"

// Diagnostic is safe-to-log provider/runtime context that may be attached to an
// AssistantMessage. It must contain metadata and redacted previews only, never
// raw request or response payloads.
type Diagnostic struct {
	Kind                string     `json:"kind,omitempty"`
	Message             string     `json:"message,omitempty"`
	Provider            ProviderID `json:"provider,omitempty"`
	API                 API        `json:"api,omitempty"`
	Model               ModelID    `json:"model,omitempty"`
	StatusCode          int        `json:"statusCode,omitempty"`
	RequestID           string     `json:"requestID,omitempty"`
	ProviderCode        string     `json:"providerCode,omitempty"`
	ProviderMessage     string     `json:"providerMessage,omitempty"`
	RetryAfterMillis    int64      `json:"retryAfterMillis,omitempty"`
	MaxRetryDelayMillis int64      `json:"maxRetryDelayMillis,omitempty"`
	BodyPreview         string     `json:"bodyPreview,omitempty"`
	UnderlyingMessage   string     `json:"underlyingMessage,omitempty"`
}

// Diagnostic returns a redacted provider diagnostic suitable for an assistant
// message that ends with StopReasonError.
func (e *ProviderError) Diagnostic() Diagnostic {
	if e == nil {
		return Diagnostic{}
	}
	diagnostic := Diagnostic{
		Kind:                string(ErrorProviderResponse),
		Message:             ErrProviderResponse.Error(),
		Provider:            e.Provider,
		API:                 e.API,
		Model:               e.Model,
		StatusCode:          e.StatusCode,
		RequestID:           redact.String(e.RequestID),
		ProviderCode:        redact.String(e.ProviderCode),
		ProviderMessage:     redact.String(e.ProviderMessage),
		RetryAfterMillis:    e.RetryAfter.Milliseconds(),
		MaxRetryDelayMillis: e.MaxRetryDelay.Milliseconds(),
		BodyPreview:         redact.Preview(e.BodyPreview, 2048),
	}
	if e.Err != nil {
		diagnostic.UnderlyingMessage = redact.String(e.Err.Error())
	}
	return diagnostic
}
