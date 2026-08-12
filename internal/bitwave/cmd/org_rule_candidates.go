package cmd

import (
	"fmt"
	"sort"
	"strings"
)

type ruleConditionCandidate struct {
	Kind             string   `json:"kind"`
	Key              string   `json:"key,omitempty"`
	Value            string   `json:"value"`
	MatchCount       int      `json:"matchCount"`
	SampleSize       int      `json:"sampleSize"`
	Coverage         float64  `json:"coverage"`
	KeyOccurrences   int      `json:"keyOccurrences,omitempty"`
	DistinctValues   int      `json:"distinctValues,omitempty"`
	Assessment       string   `json:"assessment"`
	TransactionIDs   []string `json:"transactionIds,omitempty"`
	WalletIDs        []string `json:"walletIds,omitempty"`
	TransactionTypes []string `json:"transactionTypes,omitempty"`
	NetworkIDs       []string `json:"networkIds,omitempty"`
	Assets           []string `json:"assets,omitempty"`
}

type candidateAccumulator struct {
	kind, key, value string
	transactionIDs   []string
	seen             map[string]bool
	wallets          map[string]bool
	transactionTypes map[string]bool
	networks         map[string]bool
	assets           map[string]bool
}

// ruleConditionCandidates turns raw evidence already present in transaction
// search results into bounded, deterministic hints for an LLM. It never
// chooses an accounting action; the category/contact still comes from the
// user's intent and organization context.
func ruleConditionCandidates(items []compactTransaction, limit int) []ruleConditionCandidate {
	if limit <= 0 || len(items) == 0 {
		return nil
	}
	observed := map[string]*candidateAccumulator{}
	keyOccurrences := map[string]int{}
	keyValues := map[string]map[string]bool{}
	add := func(kind, key, value string, transaction compactTransaction) {
		if strings.TrimSpace(value) == "" {
			return
		}
		identity := kind + "\x00" + key + "\x00" + value
		item := observed[identity]
		if item == nil {
			item = &candidateAccumulator{
				kind: kind, key: key, value: value, seen: map[string]bool{}, wallets: map[string]bool{},
				transactionTypes: map[string]bool{}, networks: map[string]bool{}, assets: map[string]bool{},
			}
			observed[identity] = item
		}
		if !item.seen[transaction.ID] {
			item.seen[transaction.ID] = true
			item.transactionIDs = append(item.transactionIDs, transaction.ID)
			item.transactionTypes[transaction.TransactionType] = transaction.TransactionType != ""
			for _, line := range transaction.Lines {
				item.wallets[line.WalletID] = line.WalletID != ""
				item.networks[line.NetworkID] = line.NetworkID != ""
				asset := line.AmountCurrencyName
				if asset == "" {
					asset = line.AmountCurrencyID
				}
				item.assets[asset] = asset != ""
			}
		}
	}
	for _, transaction := range items {
		if strings.TrimSpace(transaction.MethodID) != "" {
			keyOccurrences["methodId"]++
			if keyValues["methodId"] == nil {
				keyValues["methodId"] = map[string]bool{}
			}
			keyValues["methodId"][transaction.MethodID] = true
		}
		add("methodId", "", transaction.MethodID, transaction)
		for key, raw := range transaction.Metadata {
			if value, ok := scalarMetadataValue(raw); ok {
				identity := "metadata\x00" + key
				keyOccurrences[identity]++
				if keyValues[identity] == nil {
					keyValues[identity] = map[string]bool{}
				}
				keyValues[identity][value] = true
				add("metadata", key, value, transaction)
			}
		}
	}
	result := make([]ruleConditionCandidate, 0, len(observed))
	for _, item := range observed {
		count := len(item.transactionIDs)
		keyIdentity := item.kind
		if item.kind == "metadata" {
			keyIdentity += "\x00" + item.key
		}
		occurrences := keyOccurrences[keyIdentity]
		distinct := len(keyValues[keyIdentity])
		assessment := "inspect"
		if item.kind == "metadata" && likelyTransactionSpecificMetadata(item.key) {
			assessment = "avoid-transaction-specific"
		} else if occurrences >= 5 && distinct*100/occurrences >= 80 && count == 1 {
			assessment = "avoid-high-cardinality"
		} else if count >= 2 {
			assessment = "preferred-reusable-condition"
		}
		ids := item.transactionIDs
		if len(ids) > 5 {
			ids = ids[:5]
		}
		result = append(result, ruleConditionCandidate{
			Kind: item.kind, Key: item.key, Value: item.value, MatchCount: count,
			SampleSize: len(items), Coverage: float64(count) / float64(len(items)),
			KeyOccurrences: occurrences, DistinctValues: distinct,
			Assessment: assessment, TransactionIDs: ids, WalletIDs: sortedTrueKeys(item.wallets),
			TransactionTypes: sortedTrueKeys(item.transactionTypes), NetworkIDs: sortedTrueKeys(item.networks), Assets: sortedTrueKeys(item.assets),
		})
	}
	sort.Slice(result, func(i, j int) bool {
		rank := func(value string) int {
			switch value {
			case "preferred-reusable-condition":
				return 0
			case "inspect":
				return 1
			default:
				return 2
			}
		}
		if rank(result[i].Assessment) != rank(result[j].Assessment) {
			return rank(result[i].Assessment) < rank(result[j].Assessment)
		}
		if result[i].MatchCount != result[j].MatchCount {
			return result[i].MatchCount > result[j].MatchCount
		}
		left := result[i].Kind + "\x00" + strings.ToLower(result[i].Key) + "\x00" + result[i].Value
		right := result[j].Kind + "\x00" + strings.ToLower(result[j].Key) + "\x00" + result[j].Value
		return left < right
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func scalarMetadataValue(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, strings.TrimSpace(typed) != ""
	case bool, float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprint(typed), true
	default:
		return "", false
	}
}

func likelyTransactionSpecificMetadata(key string) bool {
	normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "", ".", "", " ", "").Replace(key))
	for _, fragment := range []string{"transactionid", "txid", "txhash", "hash", "blocknumber", "blockheight", "timestamp", "nonce"} {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return normalized == "id"
}
