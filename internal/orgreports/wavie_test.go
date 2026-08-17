package orgreports

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWavieSessionContracts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/orgs/org-1/wavie/sessions":
			var input CreateWavieSessionRequest
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if input.Capabilities.ClientKind != "cli" || input.Capabilities.ClientVersion != WavieProtocolVersion {
				t.Fatalf("input = %#v", input)
			}
			_, _ = w.Write([]byte(`{"sessionId":"session-1","model":"claude-opus-4-8","protocolVersion":"wavie.v1"}`))
		case "/v3/orgs/org-1/wavie/sessions/session-1/messages":
			_, _ = w.Write([]byte(`{"turnId":"1"}`))
		case "/v3/orgs/org-1/wavie/sessions/session-1/transcript":
			_, _ = w.Write([]byte(`{"entries":[{"kind":"assistant","turnId":"1","text":"hello"}]}`))
		case "/v3/orgs/org-1/wavie/sessions/session-1/interrupt":
			w.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(server.URL, func() (string, error) { return "token", nil })
	ctx := context.Background()
	session, err := client.CreateWavieSession(ctx, "org-1", "")
	if err != nil || session.SessionID != "session-1" {
		t.Fatalf("session = %#v err=%v", session, err)
	}
	turn, err := client.PostWavieMessage(ctx, "org-1", session.SessionID, "hello")
	if err != nil || turn.TurnID != "1" {
		t.Fatalf("turn = %#v err=%v", turn, err)
	}
	transcript, err := client.WavieTranscript(ctx, "org-1", session.SessionID)
	if err != nil || len(transcript.Entries) != 1 || transcript.Entries[0].Text != "hello" {
		t.Fatalf("transcript = %#v err=%v", transcript, err)
	}
	if err := client.InterruptWavieSession(ctx, "org-1", session.SessionID); err != nil {
		t.Fatal(err)
	}
}

func TestWavieClientToolContracts(t *testing.T) {
	var gotResult WavieToolResult
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/v3/orgs/org-1/wavie/sessions":
			var input CreateWavieSessionRequest
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if input.Capabilities.LocalRoot != "/work" || len(input.Capabilities.Tools) != 1 {
				t.Fatalf("capabilities = %#v", input.Capabilities)
			}
			if input.Capabilities.Tools[0].Name != "run_bitwave_cli" || input.Capabilities.Tools[0].Safety != "write" {
				t.Fatalf("tool = %#v", input.Capabilities.Tools[0])
			}
			_, _ = io.WriteString(w, `{"sessionId":"session-1","protocolVersion":"wavie.v1"}`)
		case "/v3/orgs/org-1/wavie/sessions/session-1/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "id: 1\nevent: session.ready\ndata: {\"sessionId\":\"session-1\"}\n\n")
			_, _ = io.WriteString(w, "id: 2\nevent: text.delta\ndata: {\"turnId\":\"turn-1\",\ndata: \"text\":\"hello\"}\n\n")
		case "/v3/orgs/org-1/wavie/sessions/session-1/tool-results":
			if err := json.NewDecoder(r.Body).Decode(&gotResult); err != nil {
				t.Fatal(err)
			}
			_, _ = io.WriteString(w, `{"accepted":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(server.URL, func() (string, error) { return "token", nil })
	session, err := client.CreateWavieSessionWithCapabilities(context.Background(), "org-1", CreateWavieSessionRequest{
		Capabilities: WavieCapabilities{
			ClientKind: "cli", ClientVersion: WavieProtocolVersion, LocalRoot: "/work",
			Tools: []WavieClientTool{{Name: "run_bitwave_cli", InputSchema: json.RawMessage(`{"type":"object"}`), Safety: "write"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var events []WavieEvent
	if err := client.StreamWavieSession(context.Background(), "org-1", session.SessionID, "", func(event WavieEvent) error {
		events = append(events, event)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Event != "session.ready" || string(events[1].Data) != "{\"turnId\":\"turn-1\",\n\"text\":\"hello\"}" {
		t.Fatalf("events = %#v", events)
	}
	response, err := client.PostWavieToolResult(context.Background(), "org-1", session.SessionID, WavieToolResult{
		ToolCallID: "tool-1", Status: "ok", Content: "done", Data: json.RawMessage(`{"exitCode":0}`),
	})
	if err != nil || !response.Accepted {
		t.Fatalf("response = %#v err = %v", response, err)
	}
	if gotResult.ToolCallID != "tool-1" || gotResult.Status != "ok" || gotResult.Content != "done" {
		t.Fatalf("result = %#v", gotResult)
	}
}
