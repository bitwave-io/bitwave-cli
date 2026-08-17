package orgreports

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

const WavieProtocolVersion = "wavie.v1"

type WavieCapabilities struct {
	ClientKind    string `json:"clientKind"`
	ClientVersion string `json:"clientVersion"`
	Tools         []any  `json:"tools"`
}

type CreateWavieSessionRequest struct {
	Capabilities WavieCapabilities `json:"capabilities"`
	Model        string            `json:"model,omitempty"`
}

type WavieSession struct {
	SessionID       string         `json:"sessionId"`
	Scope           map[string]any `json:"scope,omitempty"`
	Model           string         `json:"model,omitempty"`
	ProtocolVersion string         `json:"protocolVersion,omitempty"`
}

type WavieTurn struct {
	TurnID string `json:"turnId"`
}

type WavieTranscriptEntry struct {
	Kind       string `json:"kind"`
	TurnID     string `json:"turnId"`
	Iteration  int    `json:"iteration"`
	Text       string `json:"text,omitempty"`
	StopReason string `json:"stopReason,omitempty"`
}

type WavieTranscript struct {
	Entries        []WavieTranscriptEntry `json:"entries"`
	TranscriptHead string                 `json:"transcriptHead,omitempty"`
}

func (c *Client) CreateWavieSession(ctx context.Context, orgID, model string) (*WavieSession, error) {
	request := CreateWavieSessionRequest{Capabilities: WavieCapabilities{
		ClientKind: "cli", ClientVersion: WavieProtocolVersion, Tools: []any{},
	}, Model: model}
	var response WavieSession
	path := "/v3/orgs/" + url.PathEscape(orgID) + "/wavie/sessions"
	if err := c.doJSON(ctx, http.MethodPost, path, request, &response); err != nil {
		return nil, err
	}
	if response.SessionID == "" {
		return nil, fmt.Errorf("wavie session response did not include a session id")
	}
	return &response, nil
}

func (c *Client) PostWavieMessage(ctx context.Context, orgID, sessionID, message string) (*WavieTurn, error) {
	var response WavieTurn
	path := "/v3/orgs/" + url.PathEscape(orgID) + "/wavie/sessions/" + url.PathEscape(sessionID) + "/messages"
	if err := c.doJSON(ctx, http.MethodPost, path, map[string]string{"message": message}, &response); err != nil {
		return nil, err
	}
	if response.TurnID == "" {
		return nil, fmt.Errorf("wavie message response did not include a turn id")
	}
	return &response, nil
}

func (c *Client) WavieTranscript(ctx context.Context, orgID, sessionID string) (*WavieTranscript, error) {
	var response WavieTranscript
	path := "/v3/orgs/" + url.PathEscape(orgID) + "/wavie/sessions/" + url.PathEscape(sessionID) + "/transcript"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) InterruptWavieSession(ctx context.Context, orgID, sessionID string) error {
	path := "/v3/orgs/" + url.PathEscape(orgID) + "/wavie/sessions/" + url.PathEscape(sessionID) + "/interrupt"
	_, err := c.do(ctx, http.MethodPost, path, map[string]any{})
	return err
}
