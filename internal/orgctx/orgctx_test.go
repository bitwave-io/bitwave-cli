package orgctx

import "testing"

func TestLoadPrefersInvocationOrganization(t *testing.T) {
	t.Setenv("BITWAVE_ORG_ID", "org-from-sdk")
	active, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if active.OrgID != "org-from-sdk" {
		t.Fatalf("OrgID = %q, want org-from-sdk", active.OrgID)
	}
}
