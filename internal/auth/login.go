package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Login runs the full OAuth 2.0 Authorization Code + PKCE flow.
// It starts a local callback server, opens the browser, waits for the
// callback, exchanges the code for tokens, and stores them.
func Login(authBaseURL string) error {
	// 1. Generate PKCE pair.
	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		return fmt.Errorf("failed to generate PKCE pair: %w", err)
	}

	// 2. Generate state.
	state, err := GenerateState()
	if err != nil {
		return fmt.Errorf("failed to generate state: %w", err)
	}

	// 3. Start callback server.
	port, resultCh, err := startCallbackServer(state)
	if err != nil {
		return err
	}

	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)

	// 4. Build the authorization URL.
	params := url.Values{
		"client_id":             {"bw-cli"},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
		"scope":                 {"openid"},
	}
	authURL := authBaseURL + "/api/oauth/authorize?" + params.Encode()

	// 5. Open browser.
	fmt.Println("Opening browser to authenticate...")
	fmt.Printf("If the browser didn't open, visit:\n  %s\n\n", authURL)
	_ = openBrowser(authURL)

	// 6. Wait for the callback.
	fmt.Println("Waiting for authentication...")
	result := <-resultCh
	if result.Err != nil {
		return result.Err
	}

	// 7. Exchange the code for tokens.
	fmt.Println("Exchanging authorization code for tokens...")
	creds, err := ExchangeCode(authBaseURL, result.Code, redirectURI, verifier)
	if err != nil {
		return err
	}

	// 8. Store credentials.
	if err := SaveCredentials(creds); err != nil {
		return err
	}

	// 9. Print success with email from id_token.
	email := ExtractEmailFromIDToken(creds.IDToken)
	PrintLoginSuccess(email, creds.ExpiresAt)

	return nil
}

// Logout deletes stored credentials and optionally invalidates server-side session.
func Logout(authBaseURL string) error {
	creds, err := LoadCredentials()
	if err != nil {
		return err
	}
	if creds == nil {
		fmt.Println("Not logged in.")
		return nil
	}

	// Best-effort server-side logout.
	if authBaseURL != "" {
		go func() {
			client := &http.Client{Timeout: 5 * time.Second}
			_, _ = client.Get(authBaseURL + "/api/auth/logout")
		}()
	}

	if err := DeleteCredentials(); err != nil {
		return err
	}

	fmt.Println("Logged out.")
	return nil
}

// Status prints the current authentication state.
func Status() error {
	creds, err := LoadCredentials()
	if err != nil {
		return err
	}
	if creds == nil {
		fmt.Println("Not logged in.")
		return nil
	}

	email := ExtractEmailFromIDToken(creds.IDToken)
	expiresAt := time.Unix(creds.ExpiresAt, 0)

	if email != "" {
		fmt.Printf("Logged in as %s\n", email)
	} else {
		fmt.Println("Logged in.")
	}

	if creds.IsExpired() {
		fmt.Println("Token: expired (will refresh on next command)")
	} else {
		fmt.Printf("Token expires at %s\n", expiresAt.Format(time.RFC3339))
	}

	return nil
}

// ExtractEmailFromIDToken parses the JWT payload (without verification — the
// token was received directly from the auth server over HTTPS) and returns
// the "email" claim.
func ExtractEmailFromIDToken(idToken string) string {
	parts := strings.SplitN(idToken, ".", 3)
	if len(parts) < 2 {
		return ""
	}

	// JWT payload is base64url-encoded; add padding if needed.
	payload := parts[1]
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}

	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return ""
	}

	var claims struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return ""
	}
	return claims.Email
}

// openBrowser opens the given URL in the user's default browser.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	return cmd.Start()
}
