package cmd

import "testing"

func TestOrgTokenResolverCachesTokenForCommandLifetime(t *testing.T) {
	previousTokenFlag := tokenFlag
	tokenFlag = ""
	t.Cleanup(func() { tokenFlag = previousTokenFlag })
	t.Setenv("BITWAVE_AGENT_TOKEN", "")
	t.Setenv("BITWAVE_TOKEN", "first-token")

	resolve := makeOrgTokenResolver("org-1")
	first, err := resolve()
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	if first != "first-token" {
		t.Fatalf("first token = %q", first)
	}

	t.Setenv("BITWAVE_TOKEN", "replacement-token")
	second, err := resolve()
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if second != "first-token" {
		t.Fatalf("resolver did not cache token: got %q", second)
	}
}
