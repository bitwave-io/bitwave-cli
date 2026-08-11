package cmd

import (
	"fmt"
	"sort"
	"strings"
)

type ruleConditionCandidate struct {
	Kind           string   `json:"kind"`
	Key            string   `json:"key,omitempty"`
	Value          string   `json:"value"`
	MatchCount     int      `json:"matchCount"`
	SampleSize     int      `json:"sampleSize"`
	Coverage       float64  `json:"coverage"`
	Assessment     string   `json:"assessment"`
	TransactionIDs []string `json:"transactionIds,omitempty"`
}

type candidateAccumulator struct {
	kind, key, value string
	transactionIDs   []string
	seen             map[string]bool
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
	add := func(kind, key, value, transactionID string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		identity := kind + "\x00" + strings.ToLower(key) + "\x00" + value
		item := observed[identity]
		if item == nil {
			item = &candidateAccumulator{kind: kind, key: key, value: value, seen: map[string]bool{}}
			observed[identity] = item
		}
		if !item.seen[transactionID] {
			item.seen[transactionID] = true
			item.transactionIDs = append(item.transactionIDs, transactionID)
		}
	}
	for _, transaction := range items {
		add("methodId", "", transaction.MethodID, transaction.ID)
		for key, raw := range transaction.Metadata {
			if value, ok := scalarMetadataValue(raw); ok {
				add("metadata", key, value, transaction.ID)
			}
		}
	}
	result := make([]ruleConditionCandidate, 0, len(observed))
	for _, item := range observed {
		count := len(item.transactionIDs)
		assessment := "inspect"
		if item.kind == "metadata" && likelyTransactionSpecificMetadata(item.key) {
			assessment = "avoid-transaction-specific"
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
			Assessment: assessment, TransactionIDs: ids,
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
