// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

// Package safehttp keeps HTTP redirects from carrying credentials to another
// origin.
package safehttp

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// maxRedirects matches net/http's default redirect limit.
const maxRedirects = 10

// ErrUnsafeRedirect reports a redirect refused because following it would
// downgrade to plain HTTP or resend the request body to another origin.
var ErrUnsafeRedirect = errors.New("unsafe redirect refused")

// crossOriginHeaders are the only request headers kept when a redirect leaves
// the original origin. net/http strips Authorization and cookies itself, but
// forwards provider credential headers such as x-api-key and api-key.
var crossOriginHeaders = map[string]bool{
	"Accept":          true,
	"Accept-Encoding": true,
	"Accept-Language": true,
	"User-Agent":      true,
}

// Client returns a copy of client, or of http.DefaultClient when client is
// nil, whose redirects keep credentials on the original origin. Any
// CheckRedirect policy on client still runs after this one.
func Client(client *http.Client) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	safe := *client
	next := client.CheckRedirect
	safe.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := checkRedirect(req, via); err != nil {
			return err
		}
		if next != nil {
			return next(req, via)
		}
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		return nil
	}
	return &safe
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return nil
	}
	origin := via[0].URL
	if strings.EqualFold(origin.Scheme, "https") && !strings.EqualFold(req.URL.Scheme, "https") {
		return fmt.Errorf("%w: %s to %s downgrades to plain HTTP", ErrUnsafeRedirect, origin.Host, req.URL.Host)
	}
	if SameOrigin(origin, req.URL) {
		return nil
	}
	if req.Body != nil && req.Body != http.NoBody {
		return fmt.Errorf("%w: %s to %s would resend the request body", ErrUnsafeRedirect, origin.Host, req.URL.Host)
	}
	for name := range req.Header {
		if !crossOriginHeaders[http.CanonicalHeaderKey(name)] {
			delete(req.Header, name)
		}
	}
	return nil
}

// SameOrigin reports whether a and b share a scheme, host, and port, treating
// a missing port as the scheme's default.
func SameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) &&
		strings.EqualFold(a.Hostname(), b.Hostname()) &&
		effectivePort(a) == effectivePort(b)
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	default:
		return ""
	}
}
