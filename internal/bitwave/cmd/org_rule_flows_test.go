package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

func TestClusterSummaryAddressesUsesBoundedEvidenceAndFullAddress(t *testing.T) {
	const address = "0x15918ff7f6c44592c81d999b442956b07d26cc44"
	records := []orgreports.TransactionSummaryAddressRecord{
		{WalletID: "wallet-a", InteractingAddress: address, DepositsUncategorized: 70},
		{WalletID: "wallet-a", InteractingAddress: strings.ToUpper(address), DepositsUncategorized: 60},
	}

	clusters := clusterSummaryAddresses(records, "inflow", false, 2, 10)
	if len(clusters) != 1 {
		t.Fatalf("clusters = %#v", clusters)
	}
	cluster := clusters[0]
	if cluster.CounterpartyAddress != address || cluster.SuggestedRule.FromAddress != address {
		t.Fatalf("address was not preserved exactly: %#v", cluster)
	}
	if strings.Contains(cluster.CounterpartyAddress, "...") {
		t.Fatalf("address was abbreviated: %q", cluster.CounterpartyAddress)
	}
	if cluster.Count != 130 || cluster.EvidenceCount != 100 || !cluster.EvidenceSufficient {
		t.Fatalf("evidence = count:%d used:%d sufficient:%t", cluster.Count, cluster.EvidenceCount, cluster.EvidenceSufficient)
	}
	if got := strings.Join(cluster.WalletIDs, ","); got != "wallet-a" {
		t.Fatalf("wallets = %s", got)
	}
	if cluster.SuggestedRule.WalletID != "wallet-a" {
		t.Fatalf("suggested wallet = %q", cluster.SuggestedRule.WalletID)
	}
}

func TestClusterSummaryAddressesSeparatesWallets(t *testing.T) {
	const address = "0x15918ff7f6c44592c81d999b442956b07d26cc44"
	records := []orgreports.TransactionSummaryAddressRecord{
		{WalletID: "wallet-a", InteractingAddress: address, DepositsUncategorized: 4},
		{WalletID: "wallet-b", InteractingAddress: address, DepositsUncategorized: 3},
	}
	clusters := clusterSummaryAddresses(records, "inflow", false, 2, 10)
	if len(clusters) != 2 {
		t.Fatalf("clusters = %#v", clusters)
	}
	got := map[string]int{}
	for _, cluster := range clusters {
		got[cluster.SuggestedRule.WalletID] = cluster.Count
	}
	if got["wallet-a"] != 4 || got["wallet-b"] != 3 {
		t.Fatalf("wallet clusters = %#v", got)
	}
}

func TestClusterFlowTransactionsCapsStoredEvidence(t *testing.T) {
	const address = "dtc-tokenizationpilot-1::12200b406828e4e0ba69fcbf5d60b9652a4a2a153ef28c6569c0fbfa7ac899675d67"
	items := make([]json.RawMessage, 105)
	for i := range items {
		data, err := json.Marshal(flowTransaction{
			ID:              fmt.Sprintf("transaction-%03d", i),
			Timestamp:       "2026-07-01T00:00:00Z",
			TransactionType: "send",
			Lines: []compactTransactionLine{{
				AmountCurrencyID: "ETH", AmountCurrencyName: "ETH", To: address,
				WalletID: "wallet-1", Operation: "withdraw",
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		items[i] = data
	}

	clusters, skipped := clusterFlowTransactions(items, false, 2, 10)
	if skipped != 0 || len(clusters) != 1 {
		t.Fatalf("clusters=%#v skipped=%d", clusters, skipped)
	}
	cluster := clusters[0]
	if cluster.CounterpartyAddress != address || cluster.SuggestedRule.ToAddress != address {
		t.Fatalf("full non-EVM address was not preserved: %#v", cluster)
	}
	if cluster.Count != 105 || cluster.EvidenceCount != 100 || !cluster.EvidenceSufficient {
		t.Fatalf("evidence = count:%d used:%d sufficient:%t", cluster.Count, cluster.EvidenceCount, cluster.EvidenceSufficient)
	}
	if len(cluster.SampleTransactionIDs) != 5 {
		t.Fatalf("sample IDs = %d", len(cluster.SampleTransactionIDs))
	}
	if cluster.SuggestedRule.WalletID != "wallet-1" {
		t.Fatalf("suggested wallet = %q", cluster.SuggestedRule.WalletID)
	}
}
