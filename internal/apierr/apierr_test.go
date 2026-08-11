package apierr

import (
	"strings"
	"testing"
)

func TestFormatIncludesGraphQLErrorMessage(t *testing.T) {
	err := Format(400, "POST", "https://example.test/graphql", []byte(`{"errors":[{"message":"Variable prems must not be null"}]}`))
	if !strings.Contains(err.Error(), "Variable prems must not be null") {
		t.Fatalf("error = %q", err)
	}
}
