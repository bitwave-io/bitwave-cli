package operations

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bitwave-io/bitwave-cli/internal/apierr"
	"github.com/bitwave-io/bitwave-cli/internal/operation"
)

const transactionNetworkHelp = "Optional Bitwave network prefix for explorer hashes; omit for IDs returned by Bitwave"

// normalizeTransactionID preserves Bitwave's opaque identifiers, including raw
// hashes and imported/manual IDs returned by transaction search. Only an
// explicit network requests explorer-hash qualification; guessing from an ID's
// shape can select a different transaction than the caller intended.
func normalizeTransactionID(value, network string) (string, error) {
	transactionID := strings.TrimSpace(value)
	if transactionID == "" {
		return "", operation.NewValidationError("transaction ID is required")
	}
	if strings.Contains(transactionID, ".") {
		return transactionID, nil
	}

	prefix := normalizeTransactionNetwork(network)
	if prefix != "" {
		return prefix + "." + transactionID, nil
	}
	return transactionID, nil
}

func normalizeTransactionNetwork(value string) string {
	network := strings.ToUpper(strings.TrimSpace(value))
	switch network {
	case "SOLANA":
		return "SOL"
	case "ETHEREUM":
		return "ETH"
	case "BINANCE", "BINANCE-SMART-CHAIN", "BNB", "BNB-SMART-CHAIN":
		return "BSC"
	case "MATIC":
		return "POLYGON"
	case "APTOS":
		return "APT"
	default:
		return network
	}
}

// networkQualified reports whether --network turned the caller's ID into a
// network-prefixed one.
func networkQualified(raw, transactionID string) bool {
	return strings.TrimSpace(raw) != transactionID
}

func isNotFound(err error) bool {
	var apiError *apierr.Error
	return errors.As(err, &apiError) && apiError.Status == 404
}

// withNetworkPrefixHint explains a 404 for an ID that --network prefixed:
// organizations can store an explorer hash bare, so the prefixed ID is not
// found although the transaction exists. The caller's error is kept.
func withNetworkPrefixHint(err error, raw, transactionID string) error {
	if err == nil || !networkQualified(raw, transactionID) || !isNotFound(err) {
		return err
	}
	return fmt.Errorf("%w (looked up as %s because of --network; this organization may store it without a network prefix as %s, so retry without --network)", err, transactionID, strings.TrimSpace(raw))
}
