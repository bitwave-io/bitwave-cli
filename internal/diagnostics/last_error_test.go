package diagnostics

import (
	"strings"
	"testing"
)

func TestSafeURLDropsCredentialsAndQuery(t *testing.T) {
	got := safeURL("https://user:secret@example.com/v3/widgets?access_token=secret#fragment")
	if got != "https://example.com/v3/widgets" {
		t.Fatalf("safeURL = %q", got)
	}
}

func TestRedactCommonCredentialForms(t *testing.T) {
	input := `Bearer abc token=def "access_token":"ghi" client-key: jkl client_secret=mnop`
	got := redact(input)
	for _, secret := range []string{"abc", "def", "ghi", "jkl", "mnop"} {
		if strings.Contains(got, secret) {
			t.Fatalf("redact leaked %q in %q", secret, got)
		}
	}
}
