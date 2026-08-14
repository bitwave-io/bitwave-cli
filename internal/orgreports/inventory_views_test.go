package orgreports

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInventoryViewMutationContracts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/orgs/org-1/inventory-views":
			_, _ = w.Write([]byte(`{"success":true,"id":"view-1"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/orgs/org-1/inventory-views/view-1/update-requests":
			var input InventoryViewUpdateRequest
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if input.EndingDate != "2026-07-31" || !input.TransferAtHistoricalCost {
				t.Fatalf("input = %#v", input)
			}
			_, _ = w.Write([]byte(`{"success":true,"id":"update-1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/orgs/org-1/inventory-views/view-1/updates":
			_, _ = w.Write([]byte(`{"items":[{"id":"update-1","status":"Complete","inventoryViewId":"view-1"}]}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/orgs/org-1/inventory-views/view-1":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(server.URL, func() (string, error) { return "token", nil })
	ctx := context.Background()
	created, err := client.CreateInventoryView(ctx, "org-1", json.RawMessage(`{"name":"View"}`))
	if err != nil || created.ID != "view-1" {
		t.Fatalf("created = %#v err=%v", created, err)
	}
	updated, err := client.TriggerInventoryViewUpdate(ctx, "org-1", "view-1", InventoryViewUpdateRequest{EndingDate: "2026-07-31", TransferAtHistoricalCost: true})
	if err != nil || updated.ID != "update-1" {
		t.Fatalf("updated = %#v err=%v", updated, err)
	}
	updates, err := client.InventoryViewUpdates(ctx, "org-1", "view-1")
	if err != nil || len(updates) != 1 || updates[0].Status != "Complete" {
		t.Fatalf("updates = %#v err=%v", updates, err)
	}
	if err := client.DeleteInventoryView(ctx, "org-1", "view-1"); err != nil {
		t.Fatal(err)
	}
}
