// Package workspaces is a thin HTTP client for the gl-svc
// /api/v1/orgs/{orgId}/ledger/workspaces endpoints. It backs the bwx
// workspace + journal commands.
package workspaces

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
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

// Client wraps gl-svc workspace + journal HTTP endpoints.
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
		return nil, fmt.Errorf("HTTP %d %s %s: %s", resp.StatusCode, method, path, string(data))
	}
	return data, nil
}

// ListWorkspaces returns all workspaces in the active org.
func (c *Client) ListWorkspaces() ([]Workspace, error) {
	path := fmt.Sprintf("/api/v1/orgs/%s/ledger/workspaces", c.OrgId)
	data, err := c.do("GET", path, nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Workspaces []Workspace `json:"workspaces"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse list workspaces: %w", err)
	}
	return resp.Workspaces, nil
}

// CreateWorkspaceRequest is the body for POST workspaces.
type CreateWorkspaceRequest struct {
	Name         string `json:"name"`
	BaseCurrency string `json:"baseCurrency"`
}

// CreateWorkspace creates a new workspace and returns its id.
func (c *Client) CreateWorkspace(req CreateWorkspaceRequest) (string, error) {
	path := fmt.Sprintf("/api/v1/orgs/%s/ledger/workspaces", c.OrgId)
	data, err := c.do("POST", path, req)
	if err != nil {
		return "", err
	}
	var resp struct {
		Id string `json:"id"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("parse create workspace: %w", err)
	}
	return resp.Id, nil
}

// ListJournals returns the journals in a workspace.
func (c *Client) ListJournals(workspaceId string) ([]Journal, error) {
	path := fmt.Sprintf("/api/v1/orgs/%s/ledger/workspaces/%s/journals", c.OrgId, workspaceId)
	data, err := c.do("GET", path, nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Journals []Journal `json:"journals"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse list journals: %w", err)
	}
	return resp.Journals, nil
}

// CreateJournalRequest is the body for POST journals.
type CreateJournalRequest struct {
	Id          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// CreateJournal creates a journal and returns its id.
func (c *Client) CreateJournal(workspaceId string, req CreateJournalRequest) (string, error) {
	path := fmt.Sprintf("/api/v1/orgs/%s/ledger/workspaces/%s/journals", c.OrgId, workspaceId)
	data, err := c.do("POST", path, req)
	if err != nil {
		return "", err
	}
	var resp struct {
		Id string `json:"id"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("parse create journal: %w", err)
	}
	return resp.Id, nil
}
