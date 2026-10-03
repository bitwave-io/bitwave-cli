package operations

import (
	"strings"

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
