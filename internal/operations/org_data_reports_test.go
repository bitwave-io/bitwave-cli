package operations

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	op "github.com/bitwave-io/bitwave-cli/internal/operation"
	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

func TestValidateExportDateRange(t *testing.T) {
	tests := []struct {
		name     string
		from     string
		to       string
		allDates bool
		wantErr  bool
	}{
		{"valid", "2026-01-01", "2026-06-30", false, false},
		{"same day", "2026-06-30", "2026-06-30", false, false},
		{"all dates", "", "", true, false},
		{"missing end", "2026-01-01", "", false, true},
		{"backwards", "2026-07-01", "2026-06-30", false, true},
		{"invalid calendar date", "2026-02-30", "2026-06-30", false, true},
		{"ambiguous all dates", "2026-01-01", "", true, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateExportDateRange(test.from, test.to, test.allDates)
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr=%v", err, test.wantErr)
			}
		})
	}
}

func TestResolveInventoryView(t *testing.T) {
	views := []orgreports.InventoryView{
		{ID: "view-1", Name: "Primary FIFO"},
		{ID: "view-2", Name: "Secondary"},
	}
	byID, err := resolveInventoryView("view-1", views)
	if err != nil || byID.ID != "view-1" {
		t.Fatalf("by ID = %#v err=%v", byID, err)
	}
	byName, err := resolveInventoryView("primary fifo", views)
	if err != nil || byName.ID != "view-1" {
		t.Fatalf("by name = %#v err=%v", byName, err)
	}
	if _, err := resolveInventoryView("missing", views); err == nil {
		t.Fatal("expected missing-view error")
	}
}

func TestResolveWalletAndSubsidiaryNames(t *testing.T) {
	walletIDs, err := resolveWalletRefs([]string{"treasury", "wallet-2"}, []orgreports.Wallet{{ID: "wallet-1", Name: "Treasury"}, {ID: "wallet-2", Name: "Trading"}})
	if err != nil || !reflect.DeepEqual(walletIDs, []string{"wallet-1", "wallet-2"}) {
		t.Fatalf("wallet IDs = %v err=%v", walletIDs, err)
	}
	subsidiaryIDs, err := resolveSubsidiaryRefs([]string{"parent"}, []orgreports.Subsidiary{{ID: "sub-1", Name: "Parent"}})
	if err != nil || !reflect.DeepEqual(subsidiaryIDs, []string{"sub-1"}) {
		t.Fatalf("subsidiary IDs = %v err=%v", subsidiaryIDs, err)
	}
}

func TestResolveWalletRejectsAmbiguousName(t *testing.T) {
	_, err := resolveWalletRefs([]string{"Treasury"}, []orgreports.Wallet{{ID: "wallet-1", Name: "Treasury"}, {ID: "wallet-2", Name: "treasury"}})
	if err == nil || !strings.Contains(err.Error(), "wallet-1, wallet-2") {
		t.Fatalf("err = %v", err)
	}
	var validation *op.ValidationError
	if !errors.As(err, &validation) || strings.Contains(validation.Error(), "wallet-1") {
		t.Fatalf("the typed part must be static, without the candidate ids: %v", err)
	}
}

// A hosted adapter (actions-svc for Wavie) shows its caller only a typed
// ValidationError's own text and drops every wrapper: an unknown wallet
// reference, a label copied from a page, say, must therefore be typed, with
// the reference itself only in the wrapper a person at a terminal sees.
func TestResolveWalletAndSubsidiaryNotFoundAreTypedValidationErrors(t *testing.T) {
	_, err := resolveWalletRefs([]string{"TempSigner P"}, []orgreports.Wallet{{ID: "wallet-1", Name: "TempSigner P-chain 01"}})
	var validation *op.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("wallet not found must be a ValidationError, got %T: %v", err, err)
	}
	if !strings.Contains(validation.Error(), "wallet id or a wallet's exact name") || strings.Contains(validation.Error(), "TempSigner") {
		t.Fatalf("the typed text must say what --wallet takes and never echo the reference: %q", validation.Error())
	}
	if !strings.Contains(err.Error(), `"TempSigner P"`) {
		t.Fatalf("the terminal text keeps the reference: %v", err)
	}

	_, err = resolveSubsidiaryRefs([]string{"Nowhere"}, []orgreports.Subsidiary{{ID: "sub-1", Name: "Parent"}})
	if !errors.As(err, &validation) || strings.Contains(validation.Error(), "Nowhere") {
		t.Fatalf("subsidiary not found must be a static ValidationError, got %v", err)
	}
	_, err = resolveSubsidiaryRefs([]string{"Parent"}, []orgreports.Subsidiary{{ID: "sub-1", Name: "Parent"}, {ID: "sub-2", Name: "parent"}})
	if !errors.As(err, &validation) || !strings.Contains(err.Error(), "sub-1, sub-2") {
		t.Fatalf("an ambiguous subsidiary is typed and still lists the ids for the terminal: %v", err)
	}
}

func TestActionOutputPaths(t *testing.T) {
	want := []string{"/tmp/actions-part-01.csv", "/tmp/actions-part-02.csv"}
	if got := actionOutputPaths("/tmp/actions.csv", 2); !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v", got)
	}
}

func TestWriteStreamAtomicRemovesPartialOnError(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "export.csv")
	err := writeStreamAtomic(businessFileContext(t, target), target, func(w io.Writer) error {
		_, _ = io.WriteString(w, "partial")
		return errors.New("stream failed")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target should not exist: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".export.csv-*.partial"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("partial files = %v err=%v", matches, err)
	}
}
