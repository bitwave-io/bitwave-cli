package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Credentials represents the stored OAuth tokens.
type Credentials struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
}

// tokenResponse is the JSON body returned by POST /oauth/token.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error,omitempty"`
	ErrorDesc    string `json:"error_description,omitempty"`
}

// refreshBuffer is the number of seconds before expiry to trigger a refresh.
const refreshBuffer = 60

// credentialsDir returns the path to ~/.bitwave/.
func credentialsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".bitwave"), nil
}

// credentialsPath returns the full path to the credentials file.
func credentialsPath() (string, error) {
	dir, err := credentialsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials.json"), nil
}

// SaveCredentials writes tokens to ~/.bitwave/credentials.json with 0600 permissions.
func SaveCredentials(creds *Credentials) error {
	dir, err := credentialsDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}

	path, err := credentialsPath()
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write credentials: %w", err)
	}
	return nil
}

// LoadCredentials reads stored credentials. Returns nil, nil if no credentials file exists.
func LoadCredentials() (*Credentials, error) {
	path, err := credentialsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read credentials: %w", err)
	}

	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("failed to parse credentials: %w", err)
	}
	return &creds, nil
}

// DeleteCredentials removes the stored credentials file.
func DeleteCredentials() error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete credentials: %w", err)
	}
	return nil
}

// IsExpired reports whether the access token is expired or within the refresh buffer.
func (c *Credentials) IsExpired() bool {
	return time.Now().Unix()+refreshBuffer >= c.ExpiresAt
}

// ExchangeCode exchanges an authorization code for tokens via POST /oauth/token.
func ExchangeCode(authBaseURL, code, redirectURI, codeVerifier string) (*Credentials, error) {
	body := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {"bw-cli"},
		"code_verifier": {codeVerifier},
	}

	resp, err := http.Post(
		authBaseURL+"/api/oauth/token",
		"application/x-www-form-urlencoded",
		strings.NewReader(body.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read token response: %w", err)
	}

	var tok tokenResponse
	if err := json.Unmarshal(respBody, &tok); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	if tok.Error != "" {
		return nil, fmt.Errorf("token exchange error: %s — %s", tok.Error, tok.ErrorDesc)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return &Credentials{
		AccessToken:  tok.AccessToken,
		IDToken:      tok.IDToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    time.Now().Unix() + tok.ExpiresIn,
	}, nil
}

// RefreshTokens uses a refresh token to obtain new tokens.
func RefreshTokens(authBaseURL string, creds *Credentials) (*Credentials, error) {
	body := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {creds.RefreshToken},
		"client_id":     {"bw-cli"},
	}

	resp, err := http.Post(
		authBaseURL+"/api/oauth/token",
		"application/x-www-form-urlencoded",
		strings.NewReader(body.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("token refresh failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read refresh response: %w", err)
	}

	var tok tokenResponse
	if err := json.Unmarshal(respBody, &tok); err != nil {
		return nil, fmt.Errorf("failed to parse refresh response: %w", err)
	}

	if tok.Error != "" {
		// Clear credentials on refresh failure — user must re-login.
		_ = DeleteCredentials()
		return nil, fmt.Errorf("refresh failed: %s — %s\nPlease run: bw auth login", tok.Error, tok.ErrorDesc)
	}

	if resp.StatusCode != http.StatusOK {
		_ = DeleteCredentials()
		return nil, fmt.Errorf("refresh returned HTTP %d: %s\nPlease run: bw auth login", resp.StatusCode, string(respBody))
	}

	newCreds := &Credentials{
		AccessToken:  tok.AccessToken,
		IDToken:      tok.IDToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    time.Now().Unix() + tok.ExpiresIn,
	}

	if err := SaveCredentials(newCreds); err != nil {
		return nil, fmt.Errorf("refreshed tokens but failed to save: %w", err)
	}

	return newCreds, nil
}

// ClientCredentialsLogin exchanges a client_id and client_secret for tokens
// using the OAuth 2.0 client_credentials grant type. This is the headless
// alternative to the browser-based PKCE flow, intended for agents and automation.
func ClientCredentialsLogin(authBaseURL, clientID, clientSecret string) (*Credentials, error) {
	body := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
	}

	resp, err := http.Post(
		authBaseURL+"/api/oauth/token",
		"application/x-www-form-urlencoded",
		strings.NewReader(body.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("client credentials exchange failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read token response: %w", err)
	}

	var tok tokenResponse
	if err := json.Unmarshal(respBody, &tok); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	if tok.Error != "" {
		return nil, fmt.Errorf("client credentials error: %s — %s", tok.Error, tok.ErrorDesc)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("client credentials returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return &Credentials{
		AccessToken:  tok.AccessToken,
		IDToken:      tok.IDToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    time.Now().Unix() + tok.ExpiresIn,
	}, nil
}

// LoadAndRefresh loads stored credentials and refreshes them if expired.
// Returns the valid access token, or an error if not logged in or refresh fails.
func LoadAndRefresh(authBaseURL string) (string, error) {
	creds, err := LoadCredentials()
	if err != nil {
		return "", err
	}
	if creds == nil {
		return "", fmt.Errorf("not logged in — run: bw auth login")
	}

	if creds.IsExpired() {
		creds, err = RefreshTokens(authBaseURL, creds)
		if err != nil {
			return "", err
		}
	}

	return creds.AccessToken, nil
}

// LoadAndRefreshWithOrg loads stored credentials and exchanges them for an
// org-scoped enriched token (with orgId, scopes, userId claims). This is
// required for services that validate org-level permissions.
func LoadAndRefreshWithOrg(authBaseURL, orgId string) (string, error) {
	creds, err := LoadCredentials()
	if err != nil {
		return "", err
	}
	if creds == nil {
		return "", fmt.Errorf("not logged in — run: bw auth login")
	}

	body := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {creds.RefreshToken},
		"scope":         {"openid orgId:" + orgId},
	}

	resp, err := http.Post(
		authBaseURL+"/api/oauth/token",
		"application/x-www-form-urlencoded",
		strings.NewReader(body.Encode()),
	)
	if err != nil {
		return "", fmt.Errorf("org token exchange failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read org token response: %w", err)
	}

	var tok tokenResponse
	if err := json.Unmarshal(respBody, &tok); err != nil {
		return "", fmt.Errorf("failed to parse org token response: %w", err)
	}

	if tok.Error != "" {
		return "", fmt.Errorf("org token exchange failed: %s — %s", tok.Error, tok.ErrorDesc)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("org token exchange returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	newCreds := &Credentials{
		AccessToken:  tok.AccessToken,
		IDToken:      tok.IDToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    time.Now().Unix() + tok.ExpiresIn,
	}

	if err := SaveCredentials(newCreds); err != nil {
		return "", fmt.Errorf("org token obtained but failed to save: %w", err)
	}

	return newCreds.AccessToken, nil
}
