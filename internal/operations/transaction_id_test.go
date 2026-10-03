package operations

import (
	"errors"
	"strings"
	"testing"

	"github.com/bitwave-io/bitwave-cli/internal/operation"
)

func TestNormalizeTransactionID(t *testing.T) {
	solanaSignature := "4MkxKMkXFPxHRzvQtBsdn3Dn37UaVQzpcFg5SyD1wSKBNTXeAEFCjqpTBPqept9KFx9Tycto9wspspfY9MxMnBm5"
	tests := []struct {
		name    string
		value   string
		network string
		want    string
		wantErr string
	}{
		{name: "qualified", value: "SOL." + solanaSignature, want: "SOL." + solanaSignature},
		{name: "preserve solana", value: solanaSignature, want: solanaSignature},
		{name: "explicit solana alias", value: solanaSignature, network: "solana", want: "SOL." + solanaSignature},
		{name: "explicit ethereum alias", value: "0xabc", network: "ethereum", want: "ETH.0xabc"},
		{name: "explicit bnb alias", value: "0xabc", network: "bnb", want: "BSC.0xabc"},
		{name: "raw hash", value: "0xabc", want: "0xabc"},
		{name: "opaque imported ID", value: "import-123:row-42", want: "import-123:row-42"},
		{name: "manual ID", value: "manual-123", want: "manual-123"},
		{name: "trim surrounding whitespace only", value: " \tMiXeD-ID\n", want: "MiXeD-ID"},
		{name: "empty network", value: "0xabc", network: " \t", want: "0xabc"},
		{name: "already qualified", value: "BSC.0xabc", network: "ETH", want: "BSC.0xabc"},
		{name: "empty", value: " ", wantErr: "transaction ID is required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeTransactionID(test.value, test.network)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("normalizeTransactionID() error = %v, want substring %q", err, test.wantErr)
				}
				var validation *operation.ValidationError
				if !errors.As(err, &validation) {
					t.Fatalf("expected safe validation error, got %T", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeTransactionID() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("normalizeTransactionID() = %q, want %q", got, test.want)
			}
		})
	}
}
