package cmd

import (
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestInfoCatalogIncludesWholeCLISurface(t *testing.T) {
	root := NewRootCmd()
	root.InitDefaultHelpCmd()
	catalog := collectCommandInfo(root)
	paths := map[string]bool{}
	for _, item := range catalog {
		paths[item.Path] = true
	}
	for _, path := range []string{
		"bitwave help", "bitwave info", "bitwave org users", "bitwave org tokens spam",
		"bitwave org wallets disable", "bitwave transaction count", "bitwave transaction negatives",
		"bitwave lookup contract", "bitwave pricing history", "bitwave report balance-compare",
	} {
		if !paths[path] {
			t.Errorf("catalog missing %q", path)
		}
	}
}

func TestNegativeBalanceCalculationUsesExactDecimals(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rows, firstNegative, lowest := calculateNegativeBalances([]negativeLine{
		{Timestamp: start, TimeText: start.Format(time.RFC3339), Operation: "DEPOSIT", Amount: mustRat(t, "0.3"), ID: "in"},
		{Timestamp: start.Add(time.Hour), TimeText: start.Add(time.Hour).Format(time.RFC3339), Operation: "SEND", Amount: mustRat(t, "-0.1"), ID: "out-1"},
		{Timestamp: start.Add(2 * time.Hour), TimeText: start.Add(2 * time.Hour).Format(time.RFC3339), Operation: "FEE", Amount: mustRat(t, "-0.25"), ID: "out-2"},
	})
	if got := rows[len(rows)-1].Balance; got != "-0.05" {
		t.Fatalf("final balance = %q", got)
	}
	if firstNegative != start.Add(2*time.Hour).Format(time.RFC3339) || lowest != "-0.05" {
		t.Fatalf("firstNegative = %q, lowest = %q", firstNegative, lowest)
	}
}

func TestDecimalStringPreservesIntegersAndTokenPrecision(t *testing.T) {
	for input, want := range map[string]string{"10": "10", "0.000000000000000000000001": "0.000000000000000000000001", "1.2300": "1.23"} {
		if got := decimalString(mustRat(t, input)); got != want {
			t.Errorf("decimalString(%s) = %q, want %q", input, got, want)
		}
	}
}

func TestMatchingNegativeLinesNormalizesDirectionAndDeduplicates(t *testing.T) {
	raw := json.RawMessage(`{
  "id":"txn-1","timestamp":"2026-01-01T00:00:00Z","lines":[
    {"id":"line-1","walletId":"wallet-1","amountCurrencyName":"ETH","operation":"SEND","amount":"2.5"},
    {"id":"line-2","walletId":"wallet-1","amountCurrencyName":"USDC","operation":"DEPOSIT","amount":"10"}
  ]
}`)
	seen := map[string]bool{}
	lines := matchingNegativeLines(raw, "wallet-1", "eth", seen)
	if len(lines) != 1 || lines[0].Amount.RatString() != "-5/2" {
		t.Fatalf("lines = %#v", lines)
	}
	if duplicate := matchingNegativeLines(raw, "wallet-1", "ETH", seen); len(duplicate) != 0 {
		t.Fatalf("duplicate lines = %#v", duplicate)
	}
}

func TestSpamRowsUsesScoreAndEmitsEveryAddress(t *testing.T) {
	raw := json.RawMessage(`{"item":{"meta":{"symbol":"DROP","name":"Drop","spamScore":0.8,"coinId":"coin-1","addresses":[{"networkId":"eth","address":"0x1"},{"networkId":"base","address":"0x2"}]}}}`)
	rows := spamRows("DROP", raw, 0.5)
	if len(rows) != 2 || rows[0].SpamReason != "spam_score" || rows[1].NetworkID != "base" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestBalanceComparisonIncludesMissingAssetsAsZero(t *testing.T) {
	dashboard := map[string]*big.Rat{"ETH": mustRat(t, "1.5"), "BTC": mustRat(t, "2")}
	report := map[string]*big.Rat{"ETH": mustRat(t, "1.4"), "SOL": mustRat(t, "3")}
	rows := compareQuantities(dashboard, report, mustRat(t, "0.05"), false)
	if len(rows) != 3 {
		t.Fatalf("rows = %#v", rows)
	}
	var encoded strings.Builder
	for _, row := range rows {
		encoded.WriteString(row.Token + ":" + row.Difference + ";")
	}
	if got := encoded.String(); got != "BTC:-2;ETH:-0.1;SOL:3;" {
		t.Fatalf("comparison = %q", got)
	}
}

func TestPricingRangeIsLimitedTo31Days(t *testing.T) {
	if _, _, err := pricingDateRange("2026-01-01", "2026-01-31", "UTC"); err != nil {
		t.Fatalf("31-day range rejected: %v", err)
	}
	if _, _, err := pricingDateRange("2026-01-01", "2026-02-01", "UTC"); err == nil {
		t.Fatal("32-day range accepted")
	}
}

func mustRat(t *testing.T, value string) *big.Rat {
	t.Helper()
	result, ok := new(big.Rat).SetString(value)
	if !ok {
		t.Fatalf("invalid rational %q", value)
	}
	return result
}
