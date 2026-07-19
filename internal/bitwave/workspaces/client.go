// Package workspaces is a thin HTTP client for the cloud ledger workspace
// + journal endpoints (workspace-scoped /v1/workspaces surface, with the
// org-scoped /v1/orgs/{orgId}/workspaces read views). It backs the bitwave
// workspace + journal commands.
package workspaces

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/bitwave-io/bitwave-cli/internal/apierr"
)

// Workspace is the minimal shape used by the CLI.
type Workspace struct {
	Id           string `json:"id"`
	OrgId        string `json:"orgId"`
	Name         string `json:"name"`
	BaseCurrency string `json:"baseCurrency"`
}

// Journal is the minimal journal shape.
type Journal struct {
	Id          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Client wraps the cloud ledger workspace + journal HTTP endpoints.
type Client struct {
	BaseURL       string
	OrgId         string
	TokenResolver func() (string, error)
	HTTPClient    *http.Client
}

// New returns a Client with a 30s default timeout.
func New(baseURL, orgId string, tokenResolver func() (string, error)) *Client {
	return &Client{
		BaseURL:       baseURL,
		OrgId:         orgId,
		TokenResolver: tokenResolver,
		HTTPClient:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(method, path string, body any) ([]byte, error) {
	tok, err := c.TokenResolver()
	if err != nil {
		return nil, err
	}
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, apierr.Format(resp.StatusCode, method, c.BaseURL+path, data)
	}
	return data, nil
}

// parseList accepts both a bare JSON array and a `{"<key>": [...]}` wrapper,
// so the client tolerates either response framing.
func parseList[T any](data []byte, key string) ([]T, error) {
	var bare []T
	if err := json.Unmarshal(data, &bare); err == nil {
		return bare, nil
	}
	var wrapped map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrapped); err == nil {
		if raw, ok := wrapped[key]; ok {
			if err := json.Unmarshal(raw, &bare); err == nil {
				return bare, nil
			}
		}
	}
	return nil, fmt.Errorf("parse %s list", key)
}

// ListWorkspaces returns all workspaces in the active org.
func (c *Client) ListWorkspaces() ([]Workspace, error) {
	path := fmt.Sprintf("/v1/orgs/%s/workspaces", c.OrgId)
	data, err := c.do("GET", path, nil)
	if err != nil {
		return nil, err
	}
	return parseList[Workspace](data, "workspaces")
}

// CreateWorkspaceRequest is the body for POST /v1/workspaces.
type CreateWorkspaceRequest struct {
	Name         string `json:"name"`
	BaseCurrency string `json:"baseCurrency"`
	OrgId        string `json:"orgId,omitempty"`
}

// CreateWorkspace creates a new workspace and returns its id.
func (c *Client) CreateWorkspace(req CreateWorkspaceRequest) (string, error) {
	if req.OrgId == "" {
		req.OrgId = c.OrgId
	}
	data, err := c.do("POST", "/v1/workspaces", req)
	if err != nil {
		return "", err
	}
	var resp struct {
		Id string `json:"id"`
	}
	if err := json.Unmarshal(data, &resp); err != nil || resp.Id == "" {
		return "", fmt.Errorf("parse create workspace response")
	}
	return resp.Id, nil
}

// ListJournals returns the journals in a workspace.
func (c *Client) ListJournals(workspaceId string) ([]Journal, error) {
	path := fmt.Sprintf("/v1/workspaces/%s/ledger/journals", workspaceId)
	data, err := c.do("GET", path, nil)
	if err != nil {
		return nil, err
	}
	return parseList[Journal](data, "journals")
}

// CreateJournalRequest is the body for POST journals.
type CreateJournalRequest struct {
	Id          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// CreateJournal creates a journal and returns its id.
func (c *Client) CreateJournal(workspaceId string, req CreateJournalRequest) (string, error) {
	path := fmt.Sprintf("/v1/workspaces/%s/ledger/journals", workspaceId)
	data, err := c.do("POST", path, req)
	if err != nil {
		return "", err
	}
	var resp struct {
		Id string `json:"id"`
	}
	if err := json.Unmarshal(data, &resp); err != nil || resp.Id == "" {
		return "", fmt.Errorf("parse create journal response")
	}
	return resp.Id, nil
}
