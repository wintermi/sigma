// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"syscall"
	"time"
)

const (
	// DefaultMaxRetries is the default number of retries after the first HTTP
	// request attempt.
	DefaultMaxRetries = 0
	// DefaultRetryBaseDelay is the base delay used between retries when a
	// provider does not return Retry-After.
	DefaultRetryBaseDelay = 100 * time.Millisecond
	// DefaultMaxRetryDelay caps retry waits, including Retry-After.
	DefaultMaxRetryDelay = 2 * time.Second
)

// HTTPResponseHook inspects a response before retry status handling. The
// response body has not been consumed.
type HTTPResponseHook func(*http.Response) error

type retryPolicy struct {
	maxRetries    int
	baseDelay     time.Duration
	maxRetryDelay time.Duration
}

// ContextWithRequestTimeout applies Options.Timeout to ctx. The returned cancel
// function must be called when the provider request is complete.
func ContextWithRequestTimeout(ctx context.Context, opts Options) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Timeout == nil || *opts.Timeout == 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, *opts.Timeout)
}

// DoHTTPWithRetry sends a request with sigma's shared HTTP retry policy.
//
// The returned response body belongs to the caller and has not been consumed by
// the retry helper. Bodies from retry attempts are drained and closed before the
// next request attempt.
func DoHTTPWithRetry(
	ctx context.Context,
	client *http.Client,
	opts Options,
	newRequest func(context.Context) (*http.Request, error),
	providerError func(*http.Response) *ProviderError,
	hooks ...HTTPResponseHook,
) (*http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		client = http.DefaultClient
	}
	policy := retryPolicyFromOptions(opts)
	attempts := policy.attempts()

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		httpReq, err := newRequest(ctx)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(httpReq)
		if err != nil {
			if isContextError(ctx, err) || !RetryableNetworkError(err) || attempt+1 == attempts {
				return nil, err
			}
			lastErr = err
			if err := policy.wait(ctx, attempt, 0); err != nil {
				return nil, err
			}
			continue
		}

		if err := runResponseHooks(resp, hooks); err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		if !RetryableStatusCode(resp.StatusCode) || attempt+1 == attempts {
			return resp, nil
		}

		retryAfter := RetryAfter(resp.Header)
		if policy.retryAfterExceedsCap(retryAfter) {
			err := retryAfterCapError(resp, providerError, retryAfter, policy.maxRetryDelay)
			_ = resp.Body.Close()
			return nil, err
		}
		drainAndClose(resp.Body)
		if err := policy.wait(ctx, attempt, retryAfter); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

// RetryableStatusCode reports whether status is safe for pre-body-consumption
// HTTP retries.
func RetryableStatusCode(status int) bool {
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

// RetryableNetworkError reports whether err represents a transient network
// failure that occurred before an HTTP response body was returned.
func RetryableNetworkError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return true
		}
		type temporary interface {
			Temporary() bool
		}
		if temp, ok := netErr.(temporary); ok && temp.Temporary() {
			return true
		}
	}
	return false
}

// RetryAfter returns the duration requested by a Retry-After header.
func RetryAfter(header http.Header) time.Duration {
	if header == nil {
		return 0
	}
	if delay := parseRetryAfterMillis(header.Get("Retry-After-Ms")); delay > 0 {
		return delay
	}
	return ParseRetryAfter(header.Get("Retry-After"), time.Now())
}

// ParseRetryAfter parses Retry-After seconds or HTTP-date values relative to now.
func ParseRetryAfter(value string, now time.Time) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		delay := when.Sub(now)
		if delay > 0 {
			return delay
		}
	}
	return 0
}

func parseRetryAfterMillis(value string) time.Duration {
	if value == "" {
		return 0
	}
	millis, err := strconv.Atoi(value)
	if err != nil || millis <= 0 {
		return 0
	}
	return time.Duration(millis) * time.Millisecond
}

func retryPolicyFromOptions(opts Options) retryPolicy {
	policy := retryPolicy{
		maxRetries:    DefaultMaxRetries,
		baseDelay:     DefaultRetryBaseDelay,
		maxRetryDelay: DefaultMaxRetryDelay,
	}
	if opts.MaxRetries != nil {
		policy.maxRetries = *opts.MaxRetries
	}
	if opts.MaxRetryDelay != nil {
		policy.maxRetryDelay = *opts.MaxRetryDelay
	}
	return policy
}

func (p retryPolicy) attempts() int {
	if p.maxRetries < 0 {
		return 1
	}
	return p.maxRetries + 1
}

func (p retryPolicy) delay(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}
	delay := time.Duration(attempt+1) * p.baseDelay
	if p.maxRetryDelay < delay {
		delay = p.maxRetryDelay
	}
	return delay
}

func (p retryPolicy) retryAfterExceedsCap(retryAfter time.Duration) bool {
	return retryAfter > 0 && retryAfter > p.maxRetryDelay
}

func (p retryPolicy) wait(ctx context.Context, attempt int, retryAfter time.Duration) error {
	delay := p.delay(attempt, retryAfter)
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func runResponseHooks(resp *http.Response, hooks []HTTPResponseHook) error {
	for _, hook := range hooks {
		if hook == nil {
			continue
		}
		if err := hook(resp); err != nil {
			return err
		}
	}
	return nil
}

func retryAfterCapError(resp *http.Response, providerError func(*http.Response) *ProviderError, retryAfter, maxRetryDelay time.Duration) error {
	if providerError == nil {
		return &ProviderError{
			StatusCode:    resp.StatusCode,
			RetryAfter:    retryAfter,
			MaxRetryDelay: maxRetryDelay,
			Err:           ErrRetryAfterExceedsMaxDelay,
		}
	}
	err := providerError(resp)
	if err == nil {
		return ErrRetryAfterExceedsMaxDelay
	}
	err.RetryAfter = retryAfter
	err.MaxRetryDelay = maxRetryDelay
	err.Err = ErrRetryAfterExceedsMaxDelay
	return err
}

func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}

func isContextError(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		(ctx != nil && ctx.Err() != nil)
}
