package orgreports

import (
	"context"
	"encoding/json"
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
