// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

// Package sigma provides provider-neutral model calls for Go applications.
//
// The root package owns stable request, response, model, registry, stream,
// image, tool, reasoning, persistence, and error types. Provider-specific HTTP
// and cloud SDK behavior lives in provider subpackages, which register
// implementations on a Registry.
//
// Clients use a clone of the package-level default registry unless configured
// with WithRegistry. The default registry is intended for ordinary application
// code that wants built-in metadata. Provider packages still need to be imported
// and registered before runtime dispatch. Use NewRegistry with WithRegistry when
// tests, local endpoints, or applications need isolated custom providers and
// models.
//
// HTTP provider adapters share the root retry policy: no retries by default,
// optional per-request timeouts through context, retries for transient network
// failures and 408, 409, 429, and 5xx responses unless the provider's
// x-should-retry header says otherwise, and conservative streaming retries only
// before a response body is consumed.
package sigma
