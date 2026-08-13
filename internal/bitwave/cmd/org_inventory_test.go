package cmd

import (
	"strings"
	"testing"
	"time"
)

func TestUSInventoryProfilesKeepBooksAndTaxSeparate(t *testing.T) {
	profiles := usInventoryProfiles()
	if len(profiles) != 2 {
		t.Fatalf("profiles = %#v", profiles)
	}
	books, err := inventoryProfileByID(usGAAPProfile)
	if err != nil {
		t.Fatal(err)
	}
	tax, err := inventoryProfileByID(usFederalTaxFIFOProfile)
	if err != nil {
		t.Fatal(err)
	}
	if books.Purpose != "books" || !books.Request.Impair || books.Request.Config.DefaultValuationStrategy != "gaap-fair-value" || !books.Request.Config.CapitalizeTradingFees || books.Request.Config.ImpairmentMethodology != "org-default" {
		t.Fatalf("books = %#v", books)
	}
	if tax.Purpose != "tax" || tax.Request.Impair || !tax.Request.Config.CapitalizeTradingFees || tax.Request.Config.ImpairmentMethodology != "org-default" {
		t.Fatalf("tax = %#v", tax)
	}
	if books.Request.Config.InventoryMappingRule == nil || books.Request.Config.InventoryMappingRule.Type != "inventory-per-wallet" || tax.Request.Strategy.TaxStrategy != "FIFO" {
		t.Fatalf("mapping/strategy books=%#v tax=%#v", books.Request, tax.Request)
	}
}

func TestInventoryUpdateDateMatchesUIUpdateNow(t *testing.T) {
	now := time.Date(2026, time.August, 13, 1, 30, 0, 0, time.UTC)
	got, err := resolveInventoryUpdateDate("", "America/Los_Angeles", now)
	if err != nil || got != "2026-08-11" {
		t.Fatalf("date = %q err=%v", got, err)
	}
	got, err = resolveInventoryUpdateDate("2026-08-10", "America/Los_Angeles", now)
	if err != nil || got != "2026-08-10" {
		t.Fatalf("explicit date = %q err=%v", got, err)
	}
	if _, err := resolveInventoryUpdateDate("2026-08-12", "America/Los_Angeles", now); err == nil {
		t.Fatal("expected organization-local today to be rejected")
	}
}

func TestUSInventoryGuidanceIsAdvisoryAndRequiresVerification(t *testing.T) {
	for _, profile := range usInventoryProfiles() {
		if !profile.AdvisoryOnly || len(profile.Sources) < 4 || profile.LastReviewed == "" {
			t.Fatalf("profile = %#v", profile)
		}
		joined := strings.Join(append(profile.Confirmations, profile.Limitations...), " ")
		if !strings.Contains(strings.ToLower(joined), "confirm") || !strings.Contains(strings.ToLower(joined), "state") {
			t.Fatalf("guidance = %q", joined)
		}
	}
	if !strings.Contains(strings.ToLower(newOrgInventoryCmd().Long), "not legal") {
		t.Fatal("inventory help must retain the advice disclaimer")
	}
}
