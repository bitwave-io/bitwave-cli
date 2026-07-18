// Package shares is a thin HTTP client for the gl-svc-2 journal-share
// endpoints under /v1/orgs/{orgId}/journals/{journalId}/shares. It backs
// the bitwave `journal share` subcommands.
package shares

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type CreateRequest struct {
	RecipientEmail string `json:"recipientEmail"`
	Message        string `json:"message,omitempty"`
	TTLHours       int    `json:"ttlHours,omitempty"`
}

type Share struct {
	ShareId        string    `json:"shareId"`
	OrgId          string    `json:"orgId"`
	JournalId      string    `json:"journalId"`
	Token          string    `json:"token"`
	RecipientEmail string    `json:"recipientEmail"`
	Message        string    `json:"message,omitempty"`
	Status         string    `json:"status"`
	ExpiresAt      time.Time `json:"expiresAt"`
	EmailMessageId string    `json:"emailMessageId,omitempty"`
	EmailFailure   string    `json:"emailFailure,omitempty"`
	ViewCount      int64     `json:"viewCount"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type CreateResponse struct {
	Share
	URL string `json:"url"`
}

type Client struct {
	BaseURL       string
	OrgId         string
	TokenResolver func() (string, error)
	HTTPClient    *http.Client
}

func New(baseURL, orgId string, tokenResolver func() (string, error)) *Client {
	return &Client{
		BaseURL:       baseURL,
		OrgId:         orgId,
		TokenResolver: tokenResolver,
		HTTPClient:    &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) Create(ctx context.Context, journalId string, req CreateRequest) (*CreateResponse, error) {
	path := fmt.Sprintf("/v1/orgs/%s/journals/%s/shares", c.OrgId, journalId)
	body, err := c.do(ctx, http.MethodPost, path, req)
	if err != nil {
		return nil, err
	}
	var out CreateResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &out, nil
}

func (c *Client) List(ctx context.Context, journalId string) ([]Share, error) {
	path := fmt.Sprintf("/v1/orgs/%s/journals/%s/shares", c.OrgId, journalId)
	body, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var out []Share
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return out, nil
}

func (c *Client) Revoke(ctx context.Context, journalId, shareId string) (*Share, error) {
	path := fmt.Sprintf("/v1/orgs/%s/journals/%s/shares/%s/revoke", c.OrgId, journalId, shareId)
	body, err := c.do(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, err
	}
	var out Share
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &out, nil
}

func (c *Client) do(ctx context.Context, method, path string, body any) ([]byte, error) {
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
		return nil, fmt.Errorf("HTTP %d %s %s: %s", resp.StatusCode, method, path, string(data))
	}
	return data, nil
}
