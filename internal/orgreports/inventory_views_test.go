package orgreports

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInventoryViewCreateAndUpdateContracts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/orgs/org-1/inventory-views":
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s", r.Method)
			}
			var body InventoryViewCreateRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Strategy.TaxStrategy != "FIFO" || body.Config.InventoryMappingRule == nil || body.Config.InventoryMappingRule.Type != "inventory-per-wallet" || body.Config.ImpairmentMethodology != "org-default" {
				t.Fatalf("request = %#v", body)
			}
			_, _ = w.Write([]byte(`{"success":true,"id":"view-1"}`))
		case "/orgs/org-1/inventory-views/view-1/updates":
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s", r.Method)
			}
			_, _ = w.Write([]byte(`{"success":true,"id":"run-1"}`))
		case "/orgs/org-1/inventory-views/view-1":
			if r.Method != http.MethodDelete {
				t.Fatalf("method = %s", r.Method)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, func() (string, error) { return "token", nil })
	created, err := c.CreateInventoryView(context.Background(), "org-1", InventoryViewCreateRequest{
		Name:     "US Tax",
		Strategy: InventoryViewStrategy{TaxStrategy: "FIFO"},
		Config:   InventoryViewConfig{InventoryMappingRule: &InventoryMappingRule{Type: "inventory-per-wallet"}, ImpairmentMethodology: "org-default"},
	})
	if err != nil || created.ID != "view-1" {
		t.Fatalf("created = %#v err=%v", created, err)
	}
	updated, err := c.TriggerInventoryViewUpdate(context.Background(), "org-1", created.ID)
	if err != nil || updated.ID != "run-1" {
		t.Fatalf("updated = %#v err=%v", updated, err)
	}
	if err := c.DeleteInventoryView(context.Background(), "org-1", created.ID); err != nil {
		t.Fatal(err)
	}
}
