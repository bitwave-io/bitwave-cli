package sdk

import (
	"context"
	"strings"
	"testing"
)

func TestNormalizeArgsDefaultsToHelp(t *testing.T) {
	got := NormalizeArgs(nil)
	if len(got) != 1 || got[0] != "--help" {
		t.Fatalf("NormalizeArgs(nil) = %q, want [--help]", got)
	}
}

func TestExecuteBindsOrganizationToChild(t *testing.T) {
	result := Execute(context.Background(), "/usr/bin/env", t.TempDir(), []string{"--"}, "org-from-session")
	if result.ExitCode != 0 || !strings.Contains(result.Stdout, "BITWAVE_ORG_ID=org-from-session") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateArgsRejectsRecursiveWavie(t *testing.T) {
	if err := ValidateArgs([]string{"org", "wavie", "chat"}); err == nil {
		t.Fatal("expected recursive Wavie command to be rejected")
	}
}
