// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/openai"
)

const multipartResponseObject = `{"id":"resp_multipart","status":"completed","output":[{"id":"msg","type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"first ","signature":"signature"},{"type":"output_text","text":""},{"type":"refusal","refusal":"second"}]}],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}`

func TestResponsesMultipartRoutes(t *testing.T) {
	t.Parallel()
	for _, route := range []string{"responses", "azure", "codex", "deferred"} {
		t.Run(route, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if route == "deferred" {
					_, _ = w.Write([]byte(multipartResponseObject))
					return
				}
				writeResponsesSSE(t, w, "data: "+`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"first "}`+"\n\ndata: "+`{"type":"response.refusal.delta","output_index":0,"content_index":2,"delta":"second"}`+"\n\ndata: "+`{"type":"response.completed","response":`+multipartResponseObject+"}\n\n")
			}))
			t.Cleanup(server.Close)
			model := responsesTestModel(sigma.ProviderOpenAI)
			client := responsesTestClient(t, model.Provider, model, server.URL)
			var opts []sigma.Option
			switch route {
			case "azure":
				model = azureResponsesTestModel("azure-multipart")
				client = azureResponsesTestClient(t, model.Provider, model, azureAPIKeyResolver("synthetic"))
				opts = append(opts, openai.WithAzureResponsesEndpoint(model.Provider, server.URL))
			case "codex":
				model = codexResponsesTestModel("codex-multipart")
				client = codexResponsesTestClient(t, model.Provider, model, server.URL, codexTokenProvider("synthetic"))
			}
			if route == "deferred" {
				response, err := client.FetchDeferred(context.Background(), sigma.DeferredResponseHandle{ID: "resp_multipart", Provider: model.Provider, Model: model.ID, API: model.API})
				if err != nil {
					t.Fatal(err)
				}
				if response.Message == nil {
					t.Fatal("missing deferred message")
				}
				assertRouteMultipart(t, *response.Message)
				return
			}
			final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hello")}}, opts...)
			if err != nil {
				t.Fatal(err)
			}
			assertRouteMultipart(t, final)
		})
	}
}

func TestResponsesMultipartCodexWebSocket(t *testing.T) {
	// Session cleanup uses global transport state, so this test is sequential.
	openai.CloseCodexResponsesWebSocketSessions()
	t.Cleanup(openai.CloseCodexResponsesWebSocketSessions)
	server := newCodexWebSocketTestServer(t, func(_ *http.Request, ws *codexWebSocketTestConn) {
		_ = ws.readJSON(t)
		ws.writeJSON(t, map[string]any{"type": "response.output_text.delta", "output_index": 0, "content_index": 0, "delta": "first "})
		ws.writeJSON(t, map[string]any{"type": "response.refusal.delta", "output_index": 0, "content_index": 2, "delta": "second"})
		var response map[string]any
		if err := json.Unmarshal([]byte(multipartResponseObject), &response); err != nil {
			t.Error(err)
			return
		}
		ws.writeJSON(t, map[string]any{"type": "response.done", "response": response})
	})
	t.Cleanup(server.Close)
	model := codexResponsesTestModel("codex-ws-multipart")
	client := codexResponsesTestClient(t, model.Provider, model, server.URL, codexTokenProvider("synthetic"))
	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hello")}}, sigma.WithTransport(sigma.TransportWebSocket))
	if err != nil {
		t.Fatal(err)
	}
	assertRouteMultipart(t, final)
}

func assertRouteMultipart(t *testing.T, message sigma.AssistantMessage) {
	t.Helper()
	if len(message.Content) != 1 || message.Content[0].Text != "first second" || message.Content[0].Signature != "signature" || message.Content[0].ProviderMetadata["phase"] != "final_answer" {
		t.Fatalf("content = %#v", message.Content)
	}
	if message.Usage == nil || message.Usage.TotalTokens != 5 || message.StopReason != sigma.StopReasonEndTurn {
		t.Fatalf("terminal metadata = %#v", message)
	}
}
