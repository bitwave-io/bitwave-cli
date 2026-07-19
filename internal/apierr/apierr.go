// Package apierr turns raw HTTP error responses from Bitwave services into
// messages a human can act on. Servers variously return JSON error envelopes,
// bare text, or whole HTML error pages (e.g. a gateway 404) — none of which
// should be dumped verbatim into a CLI error.
package apierr

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const maxDetailLen = 200

// Format builds a readable error for a non-2xx response. method and url
// describe the request; body is the (possibly empty) response body.
func Format(status int, method, url string, body []byte) error {
	detail := extractDetail(body)
	msg := fmt.Sprintf("%s %s: %s", method, url, statusLine(status))
	if detail != "" {
		msg += ": " + detail
	}
	if hint := hintFor(status); hint != "" {
		msg += "\n  " + hint
	}
	return fmt.Errorf("%s", msg)
}

func statusLine(status int) string {
	text := http.StatusText(status)
	if text == "" {
		return fmt.Sprintf("HTTP %d", status)
	}
	return fmt.Sprintf("HTTP %d %s", status, text)
}

// extractDetail pulls a human-readable message out of the body: a JSON
// error envelope if present, plain text if short, nothing if it's HTML
// or noise.
func extractDetail(body []byte) string {
	s := strings.TrimSpace(string(body))
	if s == "" {
		return ""
	}
	// JSON envelope: try the common message fields.
	if strings.HasPrefix(s, "{") {
		var env struct {
			Error   string `json:"error"`
			Message string `json:"message"`
			Detail  string `json:"detail"`
		}
		if json.Unmarshal(body, &env) == nil {
			for _, m := range []string{env.Message, env.Error, env.Detail} {
				if m != "" {
					return truncate(m)
				}
			}
		}
		return "" // JSON with no recognizable message: omit rather than dump
	}
	// HTML error page (gateway/proxy default pages): never dump markup.
	if strings.HasPrefix(s, "<") {
		return ""
	}
	return truncate(s)
}

func truncate(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxDetailLen {
		return s[:maxDetailLen] + "…"
	}
	return s
}

func hintFor(status int) string {
	switch status {
	case http.StatusNotFound:
		return "the server doesn't expose this endpoint — it may predate this feature, or the base URL may be wrong (BITWAVE_BASE_URL_GL / --base-url)"
	case http.StatusUnauthorized:
		return "authentication required — run `bitwave auth login`, or set BITWAVE_TOKEN / BITWAVE_AGENT_TOKEN"
	case http.StatusForbidden:
		return "your identity lacks access to this resource — check the active org (`bitwave org current`)"
	default:
		return ""
	}
}
