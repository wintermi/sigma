// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"bytes"
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/jsonutil"
)

// Message threads (beta) keep a conversation on Anthropic's server, so a
// request can continue from the previous reply and send only the new
// messages. Sigma still builds the full request, compares it with the
// session's previous request, and sends only the new part when nothing
// earlier changed.

const (
	messageThreadsBeta = "message-threads-2026-08-12"
	// maxThreadSessions bounds the per-session state kept for callers that
	// never call sigma.CleanupSessionResources. Dropping the least recently
	// used session is cheap: its thread has most likely expired, and its next
	// request just starts a new one.
	maxThreadSessions = 10_000

	// textBlockType is both a text block's type and its text field name.
	textBlockType = "text"

	providerOptionDisableContinuation   = "disable_continuation"
	providerOptionDisableContinuationGo = "disableContinuation"
)

// newThreadErrors are rejections of a continue that a new thread fixes: the
// thread expired, was continued concurrently, or a fixed setting changed.
var newThreadErrors = map[string]bool{
	"thread_not_found":            true,
	"thread_already_continued":    true,
	"thread_continue_in_flight":   true,
	"thread_fingerprint_mismatch": true,
}

// threadEntry records where a session's next request can continue, or that
// the endpoint rejected threads for the session.
type threadEntry struct {
	session     string
	off         bool
	prefixCount int
	prefixHash  string
	replyID     string
}

var threadSessions = struct {
	sync.Mutex
	order   *list.List
	entries map[string]*list.Element
}{
	order:   list.New(),
	entries: make(map[string]*list.Element),
}

func init() {
	sigma.RegisterSessionResourceCleanup(func(session string) error {
		threadSessions.Lock()
		defer threadSessions.Unlock()
		if session == "" {
			threadSessions.order.Init()
			clear(threadSessions.entries)
			return nil
		}
		if element, ok := threadSessions.entries[session]; ok {
			threadSessions.order.Remove(element)
			delete(threadSessions.entries, session)
		}
		return nil
	})
}

func setThreadEntry(entry threadEntry) {
	threadSessions.Lock()
	defer threadSessions.Unlock()
	if element, ok := threadSessions.entries[entry.session]; ok {
		threadSessions.order.Remove(element)
	}
	threadSessions.entries[entry.session] = threadSessions.order.PushBack(entry)
	if threadSessions.order.Len() > maxThreadSessions {
		oldest := threadSessions.order.Front()
		threadSessions.order.Remove(oldest)
		if evicted, ok := oldest.Value.(threadEntry); ok {
			delete(threadSessions.entries, evicted.session)
		}
	}
}

// takeThreadEntry returns the session's entry, removing a continuable one so
// each reply is continued at most once, even by concurrent requests.
func takeThreadEntry(session string) (threadEntry, bool) {
	threadSessions.Lock()
	defer threadSessions.Unlock()
	element, ok := threadSessions.entries[session]
	if !ok {
		return threadEntry{}, false
	}
	entry, _ := element.Value.(threadEntry)
	if !entry.off {
		threadSessions.order.Remove(element)
		delete(threadSessions.entries, session)
	}
	return entry, true
}

// threadSession returns the session whose requests may use threads, or "".
func threadSession(opts sigma.Options, compat messagesCompat) string {
	if !compat.supportsMessageThreads || opts.SessionID == "" || !opts.CacheRetention.CacheEnabled() {
		return ""
	}
	return opts.SessionID
}

// continuationDisabled reports whether the request must not continue or
// replace the session's thread, as for a cache-warming request.
func continuationDisabled(opts sigma.Options, provider sigma.ProviderID) bool {
	options := providerOptions(opts, provider)
	for _, key := range []string{providerOptionDisableContinuation, providerOptionDisableContinuationGo} {
		if disabled, ok := options[key].(bool); ok {
			return disabled
		}
	}
	return false
}

// threadRequest is a request body with its thread field, and the full
// request it was derived from.
type threadRequest struct {
	session string
	full    map[string]any
	body    map[string]any
}

// prepareThreadRequest returns the body for session: a continue with only
// the new messages when the request extends the recorded prefix, otherwise a
// new thread. It returns nil when threads are off for the session.
func prepareThreadRequest(session string, settings string, full map[string]any) *threadRequest {
	entry, ok := takeThreadEntry(session)
	if ok && entry.off {
		return nil
	}
	messages, _ := full["messages"].([]any)
	if ok && len(messages) > entry.prefixCount && threadPrefixHash(settings, messages[:entry.prefixCount]) == entry.prefixHash {
		body := make(map[string]any, len(full))
		for key, value := range full {
			// A continue keeps the thread's system prompt and tools; the
			// prefix hash covers them, so they have not changed.
			if key != "system" && key != "tools" {
				body[key] = value
			}
		}
		body["messages"] = messages[entry.prefixCount:]
		body["thread"] = map[string]any{providerToolOptionTypeKey: "continue", "previous_message_id": entry.replyID}
		return &threadRequest{session: session, full: full, body: body}
	}
	return newThreadRequest(session, full)
}

func newThreadRequest(session string, full map[string]any) *threadRequest {
	body := make(map[string]any, len(full)+1)
	for key, value := range full {
		body[key] = value
	}
	body["thread"] = map[string]any{providerToolOptionTypeKey: "create"}
	return &threadRequest{session: session, full: full, body: body}
}

// threadErrorCode returns the error_code Anthropic puts in error.details of
// thread rejections.
func threadErrorCode(body []byte) string {
	var decoded struct {
		Error struct {
			Details struct {
				ErrorCode string `json:"error_code"`
			} `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &decoded)
	return decoded.Error.Details.ErrorCode
}

// threadFallback decides how to resend a rejected thread request: as a new
// thread when the old one cannot be continued, or without a thread, which
// turns threads off for the session, when the endpoint cannot use them.
// Other rejections are not about the thread and are returned as is.
func threadFallback(status int, body []byte) (newThread bool, withoutThread bool) {
	code := threadErrorCode(body)
	if newThreadErrors[code] {
		return true, false
	}
	unsupported := code == "thread_unsupported_request" ||
		(code == "" && status == 400 && bytes.Contains(bytes.ToLower(body), []byte("thread")))
	return false, unsupported
}

// recordThreadReply records where the session's next request can continue:
// after reply, the answer to the thread request whose full request was
// thread.full. replyMessages are the full request's messages followed by the
// reply, converted as the next request will send them.
func recordThreadReply(thread *threadRequest, settings string, replyID string, replyMessages []any) {
	if replyID == "" || len(replyMessages) == 0 {
		return
	}
	setThreadEntry(threadEntry{
		session:     thread.session,
		prefixCount: len(replyMessages),
		prefixHash:  threadPrefixHash(settings, replyMessages),
		replyID:     replyID,
	})
}

func turnThreadsOff(session string) {
	setThreadEntry(threadEntry{session: session, off: true})
}

// threadSettings serializes what a continue builds on besides messages: the
// endpoint, model, and the fields a continue omits or must keep unchanged.
func threadSettings(endpoint string, beta string, full map[string]any) string {
	settings, _ := json.Marshal([]any{
		endpoint, full["model"], full["system"], full["tools"], full["thinking"],
		full["output_config"], full["tool_choice"], beta,
	})
	return string(stripCacheControl(settings))
}

// threadPrefixHash hashes settings and messages, ignoring cache markers,
// which move between requests without changing what a message means.
func threadPrefixHash(settings string, messages []any) string {
	encoded, _ := json.Marshal(messages)
	sum := sha256.Sum256(append([]byte(settings), stripCacheControl(encoded)...))
	return hex.EncodeToString(sum[:])
}

func stripCacheControl(encoded []byte) []byte {
	var value any
	if err := jsonutil.Decode(encoded, &value); err != nil {
		return encoded
	}
	stripped, err := json.Marshal(withoutCacheControl(value))
	if err != nil {
		return encoded
	}
	return stripped
}

func withoutCacheControl(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if key == "cache_control" {
				continue
			}
			// A message's string content means the same as one text block.
			if text, ok := item.(string); ok && key == "content" {
				out[key] = []any{map[string]any{providerToolOptionTypeKey: textBlockType, textBlockType: text}}
				continue
			}
			out[key] = withoutCacheControl(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = withoutCacheControl(item)
		}
		return out
	default:
		return value
	}
}

// normalizedPayload returns payload as decoded JSON, keeping exact numbers.
func normalizedPayload(payload map[string]any) (map[string]any, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := jsonutil.Decode(encoded, &out); err != nil {
		return nil, err
	}
	return out, nil
}
