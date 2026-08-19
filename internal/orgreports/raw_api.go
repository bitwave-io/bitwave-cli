package orgreports

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// RawResponse preserves response metadata needed by first-class administrative
// commands. In particular, core-svc uses ETags to protect organization and
// identity settings from lost updates.
type RawResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

const (
	APIServiceCore     = "core"
	APIServiceApp      = "app"
	APIServicePlatform = "platform"
	APIServiceReports  = "reports"
)

// RawRequest sends an authenticated request to one of Bitwave's known API
// services. The path must be relative so an organization token can never be
// forwarded to an arbitrary host.
func (c *Client) RawRequest(ctx context.Context, service, method, path string, body any) ([]byte, error) {
	endpoint, err := c.RawEndpoint(service, path)
	if err != nil {
		return nil, err
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = http.MethodGet
	}
	return c.doEndpoint(ctx, method, endpoint, body, true)
}

// RawRequestBytes sends an exact request body and caller-provided headers to a
// known Bitwave service. Authentication is still owned by the client.
func (c *Client) RawRequestBytes(ctx context.Context, service, method, path string, body []byte, headers http.Header) ([]byte, error) {
	endpoint, err := c.RawEndpoint(service, path)
	if err != nil {
		return nil, err
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = http.MethodGet
	}
	return c.doEndpointBytes(ctx, method, endpoint, body, true, headers)
}

// RawRequestDetailed is RawRequestBytes with status and response headers.
func (c *Client) RawRequestDetailed(ctx context.Context, service, method, path string, body []byte, headers http.Header) (*RawResponse, error) {
	endpoint, err := c.RawEndpoint(service, path)
	if err != nil {
		return nil, err
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = http.MethodGet
	}
	return c.doEndpointBytesDetailed(ctx, method, endpoint, body, true, headers)
}

// RawEndpoint resolves a relative API path against a fixed Bitwave service.
// It is exported so the CLI can show the exact request during a dry run.
func (c *Client) RawEndpoint(service, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("API path is required")
	}
	relative, err := url.Parse(path)
	if err != nil {
		return "", fmt.Errorf("invalid API path: %w", err)
	}
	if relative.IsAbs() || relative.Host != "" || strings.HasPrefix(path, "//") {
		return "", errors.New("API path must be relative; arbitrary URLs are not allowed")
	}
	if relative.Fragment != "" {
		return "", errors.New("API path must not contain a fragment")
	}
	if !strings.HasPrefix(relative.Path, "/") {
		relative.Path = "/" + relative.Path
	}

	base, err := c.rawServiceBase(service)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(base, "/") + relative.String(), nil
}

func (c *Client) rawServiceBase(service string) (string, error) {
	var endpoint string
	switch strings.ToLower(strings.TrimSpace(service)) {
	case "", APIServiceCore:
		return validateRawServiceBase(c.BaseURL, service)
	case APIServiceApp:
		endpoint = c.RulesMutationURL
	case APIServicePlatform:
		endpoint = c.RulesQueryURL
	case APIServiceReports:
		endpoint = c.RulesQueryURL
	default:
		return "", fmt.Errorf("unsupported API service %q (use core, app, platform, or reports)", service)
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid %s API service URL", service)
	}
	core, coreErr := url.Parse(c.BaseURL)
	if coreErr == nil && strings.EqualFold(parsed.Scheme, core.Scheme) && strings.EqualFold(parsed.Host, core.Host) {
		// Local and self-hosted configurations build GraphQL URLs by appending
		// to the configured core prefix. Keep that prefix for arbitrary calls.
		return strings.TrimRight(c.BaseURL, "/"), nil
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

func validateRawServiceBase(base, service string) (string, error) {
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid %s API service URL", service)
	}
	return strings.TrimRight(base, "/"), nil
}
