// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package streamstate

import "context"

type textIdentityKey struct{}

// TextIdentity identifies the request when cancellation precedes provider events.
type TextIdentity struct {
	Provider string
	Model    string
}

// WithTextIdentity seeds identity before a stream producer starts.
func WithTextIdentity(ctx context.Context, provider, model string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, textIdentityKey{}, TextIdentity{Provider: provider, Model: model})
}

// TextIdentityFromContext returns the identity, or zero values for standalone streams.
func TextIdentityFromContext(ctx context.Context) TextIdentity {
	if ctx == nil {
		return TextIdentity{}
	}
	identity, _ := ctx.Value(textIdentityKey{}).(TextIdentity)
	return identity
}
