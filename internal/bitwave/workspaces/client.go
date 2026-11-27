// Package workspaces is a thin HTTP client for the cloud ledger workspace
// + journal endpoints. Every workspace is addressed inside its org:
// /v1/orgs/{orgId}/workspaces[/{workspaceId}/...]. It backs the bitwave
// workspace + journal commands.
package workspaces

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/bitwave-io/bitwave-cli/internal/apierr"
)

// Workspace is the minimal shape used by the CLI.
type Workspace struct {
	Id           string `json:"id"`
	OrgId        string `json:"orgId"`
	Name         string `json:"name"`
	BaseCurrency string `json:"baseCurrency"`
	// URL is the browser-facing workspace URL. Empty on older servers that
	// predate the field; callers must handle absence gracefully.
	URL string `json:"url"`
}

// Journal is the minimal journal shape.
type Journal struct {
	Id          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ErrMissingOrg is returned when a workspace request is attempted without an
// org: the workspace ledger only serves workspaces inside their org.
var ErrMissingOrg = errors.New("workspace ledger requests need an org id")

// Client wraps the cloud ledger workspace + journal HTTP endpoints. OrgId is
// the org every request is addressed to.
type Client struct {
	BaseURL       string
	OrgId         string
	TokenResolver func() (string, error)
	HTTPClient    *http.Client
	Context       context.Context
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
	ctx := c.Context
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, r)
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

// orgPath builds /v1/orgs/{orgId}/workspaces followed by the escaped parts.
func (c *Client) orgPath(parts ...string) (string, error) {
	if c.OrgId == "" {
		return "", ErrMissingOrg
	}
	path := "/v1/orgs/" + url.PathEscape(c.OrgId) + "/workspaces"
	for _, part := range parts {
		path += "/" + url.PathEscape(part)
	}
	return path, nil
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
	path, err := c.orgPath()
	if err != nil {
		return nil, err
	}
	data, err := c.do("GET", path, nil)
	if err != nil {
		return nil, err
	}
	return parseList[Workspace](data, "workspaces")
}

// CreateWorkspaceRequest is the body for POST /v1/orgs/{orgId}/workspaces.
type CreateWorkspaceRequest struct {
	Name         string `json:"name"`
	BaseCurrency string `json:"baseCurrency"`
	// NotifyEmail, when set, asks the server to email the creator a link to
	// the new workspace. Omitted from the request when empty (e.g. agent
	// identities with no resolvable email).
	NotifyEmail string `json:"notifyEmail,omitempty"`
}

// CreateWorkspaceResult is what CreateWorkspace returns: the new workspace id
// and its browser-facing URL. URL is empty on servers that predate the field.
type CreateWorkspaceResult struct {
	Id  string
	URL string
}

// CreateWorkspace creates a new workspace in the client's org and returns its
// id and URL.
func (c *Client) CreateWorkspace(req CreateWorkspaceRequest) (CreateWorkspaceResult, error) {
	path, err := c.orgPath()
	if err != nil {
		return CreateWorkspaceResult{}, err
	}
	data, err := c.do("POST", path, req)
	if err != nil {
		return CreateWorkspaceResult{}, err
	}
	var resp struct {
		Id  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal(data, &resp); err != nil || resp.Id == "" {
		return CreateWorkspaceResult{}, fmt.Errorf("parse create workspace response")
	}
	return CreateWorkspaceResult{Id: resp.Id, URL: resp.URL}, nil
}

// GetWorkspace fetches a single workspace via
// GET /v1/orgs/{orgId}/workspaces/{workspaceId}.
func (c *Client) GetWorkspace(workspaceId string) (Workspace, error) {
	path, err := c.orgPath(workspaceId)
	if err != nil {
		return Workspace{}, err
	}
	data, err := c.do("GET", path, nil)
	if err != nil {
		return Workspace{}, err
	}
	var w Workspace
	if err := json.Unmarshal(data, &w); err != nil || w.Id == "" {
		return Workspace{}, fmt.Errorf("parse workspace response")
	}
	return w, nil
}

// ListJournals returns the journals in a workspace.
func (c *Client) ListJournals(workspaceId string) ([]Journal, error) {
	path, err := c.orgPath(workspaceId, "ledger", "journals")
	if err != nil {
		return nil, err
	}
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
	path, err := c.orgPath(workspaceId, "ledger", "journals")
	if err != nil {
		return "", err
	}
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
