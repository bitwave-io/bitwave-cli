package orgreports

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const WavieProtocolVersion = "wavie.v1"

type WavieCapabilities struct {
	ClientKind    string            `json:"clientKind"`
	ClientVersion string            `json:"clientVersion"`
	LocalRoot     string            `json:"localRoot,omitempty"`
	Tools         []WavieClientTool `json:"tools"`
}

type WavieClientTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	Safety      string          `json:"safety,omitempty"`
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

type WavieEvent struct {
	ID    string
	Event string
	Data  json.RawMessage
}

type WavieToolResult struct {
	ToolCallID string          `json:"toolCallId"`
	Status     string          `json:"status"`
	Content    string          `json:"content,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
}

type WavieToolResultResponse struct {
	Accepted bool `json:"accepted"`
}

func (c *Client) CreateWavieSession(ctx context.Context, orgID, model string) (*WavieSession, error) {
	request := CreateWavieSessionRequest{Capabilities: WavieCapabilities{
		ClientKind: "cli", ClientVersion: WavieProtocolVersion, Tools: []WavieClientTool{},
	}, Model: model}
	return c.CreateWavieSessionWithCapabilities(ctx, orgID, request)
}

func (c *Client) CreateWavieSessionWithCapabilities(ctx context.Context, orgID string, request CreateWavieSessionRequest) (*WavieSession, error) {
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

func (c *Client) PostWavieToolResult(ctx context.Context, orgID, sessionID string, result WavieToolResult) (*WavieToolResultResponse, error) {
	var response WavieToolResultResponse
	path := "/v3/orgs/" + url.PathEscape(orgID) + "/wavie/sessions/" + url.PathEscape(sessionID) + "/tool-results"
	if err := c.doJSON(ctx, http.MethodPost, path, result, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// StreamWavieSession attaches to the wavie.v1 Server-Sent Events stream and
// calls handle for every complete event. The caller controls its lifetime by
// cancelling ctx. lastEventID may be supplied when reconnecting.
func (c *Client) StreamWavieSession(ctx context.Context, orgID, sessionID, lastEventID string, handle func(WavieEvent) error) error {
	endpoint := c.BaseURL + "/v3/orgs/" + url.PathEscape(orgID) + "/wavie/sessions/" + url.PathEscape(sessionID) + "/stream"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	token, err := c.TokenResolver()
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "text/event-stream")
	if strings.TrimSpace(lastEventID) != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}

	httpClient := *c.HTTPClient
	httpClient.Timeout = 0
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("read Wavie stream error response: %w", readErr)
		}
		return fmt.Errorf("stream Wavie session: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	var event WavieEvent
	var dataLines []string
	deliver := func() error {
		if event.Event == "" && event.ID == "" && len(dataLines) == 0 {
			return nil
		}
		event.Data = json.RawMessage(strings.Join(dataLines, "\n"))
		if err := handle(event); err != nil {
			return err
		}
		event = WavieEvent{}
		dataLines = nil
		return nil
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := deliver(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if found {
			value = strings.TrimPrefix(value, " ")
		}
		switch field {
		case "id":
			event.ID = value
		case "event":
			event.Event = value
		case "data":
			dataLines = append(dataLines, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return deliver()
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
