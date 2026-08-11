// Package rulerecipes contains the compact, versioned Bitwave rule knowledge
// exposed to agents by `bitwave rule recipes` and used by `rule plan/apply`.
package rulerecipes

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	SchemaVersion      = "1"
	SourceURL          = "https://docs.bitwave.io/docs/set-up-categorization-rules"
	MetadataSourceURL  = "https://docs.bitwave.io/docs/metadata-based-rule-categorization"
	RuleUsageSourceURL = "https://docs.bitwave.io/docs/how-to-use-rules"
	LastVerified       = "2026-08-11"
)

type Source struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

func Sources() []Source {
	return []Source{
		{Title: "Set Up Categorization Rules", URL: SourceURL},
		{Title: "Metadata Based Rule Categorization", URL: MetadataSourceURL},
		{Title: "How to Use Rules", URL: RuleUsageSourceURL},
	}
}

type Field struct {
	Name        string `json:"name"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description"`
}

type Recipe struct {
	Name             string         `json:"name"`
	Summary          string         `json:"summary"`
	ActionType       string         `json:"actionType"`
	PlanningTier     int            `json:"planningTier"`
	DefaultScope     string         `json:"defaultScope"`
	DefaultDirection string         `json:"defaultDirection"`
	DefaultMulti     bool           `json:"defaultMultiToken"`
	ApplySupported   bool           `json:"applySupported"`
	Fields           []Field        `json:"fields"`
	Defaults         map[string]any `json:"defaults"`
	Guidance         []string       `json:"guidance"`
}

type PlanningTier struct {
	Tier               int      `json:"tier"`
	Name               string   `json:"name"`
	Presets            []string `json:"presets"`
	RecommendedStart   bool     `json:"recommendedStart"`
	ApplyAsSingleBatch bool     `json:"applyAsSingleBatch"`
	Guidance           []string `json:"guidance"`
}

func PlanningHierarchy() []PlanningTier {
	return []PlanningTier{
		{
			Tier: 1, Name: "organization-wide transaction type", RecommendedStart: true, ApplyAsSingleBatch: true,
			Presets: []string{"trade", "internal-transfer", "gas-fee-only"},
			Guidance: []string{
				"Always suggest this tier as the first rule setup after accounting resources are ready; create all three applicable defaults unless the user declines or equivalent rules already exist.",
				"These defaults do not require a transaction-history scan. Check existing rules, resolve the Gas Fees category/contact, and apply the missing rules in one batch.",
				"After creation, trigger `bitwave rule run --yes`; background rule processing is intermittent (approximately twice daily), so creation alone does not apply rules immediately.",
				"Create one organization-wide rule for each applicable transaction type; omit wallet, asset, and address filters by default.",
				"Trade rules keep ignoreFailPricing=false so failed-priced transactions, including possible DeFi activity, are not swept into the generic trade rule.",
			},
		},
		{
			Tier: 2, Name: "granular deposit and withdrawal behavior",
			Presets: []string{"simple-inflow", "simple-outflow", "metadata-categorization", "detailed-categorization"},
			Guidance: []string{
				"Direction alone is insufficient; inspect transaction evidence and narrow with stable metadata, method ID, address, asset, wallet, or another supported condition.",
				"Simple inflow and outflow rules should be wallet-scoped by default. Organization-wide scope is supported, but it should be a deliberate exception.",
			},
		},
	}
}

var catalog = []Recipe{
	{
		Name: "simple-inflow", Summary: "Categorize matching inbound transactions to one category and contact.",
		ActionType: "SimpleCategorization", PlanningTier: 2, DefaultScope: "wallet", DefaultDirection: "Inbound", ApplySupported: true,
		Fields:   []Field{{"category", true, "Category ID or exact name."}, {"contact", true, "Contact ID or exact name."}, {"asset", false, "Single asset required for the match."}},
		Defaults: map[string]any{"multiToken": false, "autoCategorizeFee": true, "allowMismatch": false},
		Guidance: []string{"Set a wallet by default; the same asset and counterparty can require different treatment in another wallet. Use organization-wide scope only as a deliberate exception.", "Use multiToken=false when --asset identifies a single-token rule.", "Use from/to address filters only for primary transaction addresses, not token transfer lines.", "Network fees are separate FEE lines. Supply a fee category and fee contact when autoCategorizeFee=true; never silently post fees to the inflow category."},
	},
	{
		Name: "simple-outflow", Summary: "Categorize matching outbound transactions to one category and contact.",
		ActionType: "SimpleCategorization", PlanningTier: 2, DefaultScope: "wallet", DefaultDirection: "Outbound", ApplySupported: true,
		Fields:   []Field{{"category", true, "Category ID or exact name."}, {"contact", true, "Contact ID or exact name."}, {"asset", false, "Single asset required for the match."}},
		Defaults: map[string]any{"multiToken": false, "autoCategorizeFee": true, "allowMismatch": false},
		Guidance: []string{"Set a wallet by default; the same asset and counterparty can require different treatment in another wallet. Use organization-wide scope only as a deliberate exception.", "Use multiToken=false when --asset identifies a single-token rule.", "Network fees are separate FEE lines. Supply a fee category and fee contact when autoCategorizeFee=true; never silently post fees to the outflow category."},
	},
	{
		Name: "trade", Summary: "Categorize swap/trade transactions and their fee contact.",
		ActionType: "TradeCategorization", PlanningTier: 1, DefaultScope: "organization", DefaultDirection: "All", DefaultMulti: true, ApplySupported: true,
		Fields:   []Field{{"feeContact", true, "Required contact for the trade fee. Trades do not take a fee category."}},
		Defaults: map[string]any{"multiToken": true, "autoCategorizeFee": false, "allowMismatch": true, "ignoreFailPricing": false},
		Guidance: []string{"Create one organization-wide trade rule; omit wallet, asset, address, and date filters by default.", "Keep ignoreFailPricing=false (the checkbox unchecked). Failed-priced transactions can represent DeFi activity and should not be swept into the generic trade rule.", "Trades use multiToken=true because Bitwave trades exchange one asset for one or more assets.", "A trade fee requires feeContactId but no feeCategoryId; leaving autoCategorizeFee=false keeps the fee in trade treatment so it can be capitalized."},
	},
	{
		Name: "internal-transfer", Summary: "Categorize wallet-to-wallet transfers and associated fees.",
		ActionType: "InternalTransferCategorization", PlanningTier: 1, DefaultScope: "organization", DefaultDirection: "All", ApplySupported: true,
		Fields:   []Field{{"feeCategory", true, "Fee category ID or exact name."}, {"feeContact", true, "Fee contact ID or exact name."}},
		Defaults: map[string]any{"multiToken": false, "autoCategorizeFee": true, "allowMismatch": false},
		Guidance: []string{"Create one organization-wide internal-transfer rule and omit wallet filters by default.", "Enable multi-token only when matching transfers contain more than one transferred asset.", "Add wallet scope only for an explicit exception where treatment genuinely differs."},
	},
	{
		Name: "gas-fee-only", Summary: "Categorize transactions containing only network/contract execution fees.",
		ActionType: "DetailedCategorization", PlanningTier: 1, DefaultScope: "organization", DefaultDirection: "Outbound", ApplySupported: true,
		Fields:   []Field{{"feeCategory", true, "Category used for the gas fee."}, {"feeContact", true, "Contact used for the gas fee."}},
		Defaults: map[string]any{"multiToken": false, "autoCategorizeFee": true, "allowMismatch": false},
		Guidance: []string{"Create one organization-wide gas-fee-only rule and omit wallet filters by default.", "Gas Fee Only is a Standard, Outbound, Advanced Categorize > Detailed Categorize rule; it is not an Internal Transfer rule or the disabled Fee Only transaction-type tab.", "Its detailed line must extract value=fee and asset=COIN into the gas category and contact.", "Add wallet scope only for an explicit exception where treatment genuinely differs."},
	},
	{
		Name: "ignore-blank", Summary: "Ignore transactions that contain no transferred value.",
		ActionType: "Ignore", PlanningTier: 2, DefaultScope: "transaction-specific", DefaultDirection: "Empty", ApplySupported: true,
		Fields: []Field{}, Defaults: map[string]any{"multiToken": false, "autoCategorizeFee": false, "allowMismatch": false},
		Guidance: []string{"Use a bounded date window first because enabled rules also affect historical data.", "Do not use this preset for failed pricing or otherwise non-blank economic activity."},
	},
	{
		Name: "metadata-categorization", Summary: "Categorize transactions matching one or more metadata key/value conditions.",
		ActionType: "SimpleCategorization", PlanningTier: 2, DefaultScope: "transaction-specific", DefaultDirection: "All", ApplySupported: true,
		Fields:   []Field{{"metadata", true, "One or more observed transaction metadata key/value pairs."}, {"methodId", false, "Observed smart-contract method ID."}, {"category", true, "Category ID or exact name."}, {"contact", true, "Contact ID or exact name."}},
		Defaults: map[string]any{"metadataOperator": "AND", "multiToken": false, "autoCategorizeFee": true, "allowMismatch": false},
		Guidance: []string{"Prefer stable, repeated metadata or methodId conditions whenever sampled transaction data exposes them; this applies across networks, not only Canton.", "Metadata conditions and methodId can also be attached to any other supported preset.", "Use wallet, address, direction, and asset only to disambiguate or narrow a metadata/methodId rule.", "Do not turn transaction-specific metadata such as hashes, block numbers, timestamps, IDs, or nonces into reusable rules.", "Choose multi-token handling from the sampled transactions; metadata alone does not imply it."},
	},
	{
		Name: "detailed-categorization", Summary: "Categorize token-level or multi-line transaction details using extractor lines.",
		ActionType: "DetailedCategorization", PlanningTier: 2, DefaultScope: "transaction-specific", DefaultDirection: "All", ApplySupported: false,
		Fields:   []Field{{"rawInput", true, "Use `rule create --input` until detailed line flags are implemented."}},
		Defaults: map[string]any{"multiToken": true},
		Guidance: []string{"Use detailed rules for token transfer-line addresses and for splitting multiple lines to different categories/contacts.", "The compact preset intentionally does not guess extractor-line semantics."},
	},
}

func List() []Recipe {
	items := append([]Recipe(nil), catalog...)
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

func Find(name string) (Recipe, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, recipe := range catalog {
		if recipe.Name == name {
			return recipe, true
		}
	}
	return Recipe{}, false
}

// Plan is a fully resolved recipe. Human labels have already been converted
// to stable Bitwave IDs before this package builds the GraphQL Rule envelope.
type Plan struct {
	Preset                    string
	ID                        string
	Name                      string
	Priority                  int
	AccountingConnectionID    string
	CategoryID                string
	ContactID                 string
	FeeCategoryID             string
	FeeContactID              string
	Asset                     string
	MethodID                  string
	Direction                 string
	WalletID                  string
	FromAddress               string
	ToAddress                 string
	AfterDateSEC              int64
	BeforeDateSEC             int64
	Enabled                   bool
	MultiToken                *bool
	AutoCategorizeFee         *bool
	AllowMismatch             *bool
	IgnoreFailPricing         bool
	Metadata                  []MetadataPair
	MetadataOperator          string
	MetadataTransactionRecord bool
}

type MetadataPair struct {
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

func Build(plan Plan) (json.RawMessage, error) {
	recipe, ok := Find(plan.Preset)
	if !ok {
		return nil, fmt.Errorf("unknown rule preset %q", plan.Preset)
	}
	if !recipe.ApplySupported {
		return nil, fmt.Errorf("preset %q is guidance-only; use `bitwave rule create --input`", recipe.Name)
	}
	if strings.TrimSpace(plan.Name) == "" {
		return nil, fmt.Errorf("rule name is required")
	}
	if plan.Priority < 1 || plan.Priority > 10 {
		return nil, fmt.Errorf("rule priority must be between 1 and 10")
	}
	if plan.AccountingConnectionID == "" {
		return nil, fmt.Errorf("accounting connection is required")
	}
	direction := plan.Direction
	if direction == "" {
		direction = recipe.DefaultDirection
	}
	if direction != "Inbound" && direction != "Outbound" && direction != "All" && direction != "Empty" && direction != "NA" {
		return nil, fmt.Errorf("unsupported rule direction %q", direction)
	}
	multiToken := recipe.DefaultMulti
	if plan.MultiToken != nil {
		multiToken = *plan.MultiToken
	}
	autoCategorizeFee, _ := recipe.Defaults["autoCategorizeFee"].(bool)
	if plan.AutoCategorizeFee != nil {
		autoCategorizeFee = *plan.AutoCategorizeFee
	}
	allowMismatch, _ := recipe.Defaults["allowMismatch"].(bool)
	if plan.AllowMismatch != nil {
		allowMismatch = *plan.AllowMismatch
	}

	action := map[string]any{"type": recipe.ActionType}
	switch recipe.Name {
	case "simple-inflow", "simple-outflow", "metadata-categorization":
		if plan.CategoryID == "" || plan.ContactID == "" {
			return nil, fmt.Errorf("preset %q requires category and contact", recipe.Name)
		}
		action["categoryId"] = plan.CategoryID
		action["contactId"] = plan.ContactID
		if plan.FeeCategoryID != "" {
			action["feeCategoryId"] = plan.FeeCategoryID
		}
		if plan.FeeContactID != "" {
			action["feeContactId"] = plan.FeeContactID
		}
		action["ignoreFailPricing"] = plan.IgnoreFailPricing
	case "trade":
		if plan.FeeContactID == "" {
			return nil, fmt.Errorf("preset trade requires fee contact")
		}
		action["feeContactId"] = plan.FeeContactID
		action["ignoreFailPricing"] = plan.IgnoreFailPricing
	case "internal-transfer":
		if plan.FeeCategoryID == "" || plan.FeeContactID == "" {
			return nil, fmt.Errorf("preset %q requires fee category and fee contact", recipe.Name)
		}
		action["feeCategoryId"] = plan.FeeCategoryID
		action["feeContactId"] = plan.FeeContactID
		action["ignoreFailPricing"] = plan.IgnoreFailPricing
	case "gas-fee-only":
		if plan.FeeCategoryID == "" || plan.FeeContactID == "" {
			return nil, fmt.Errorf("preset %q requires fee category and fee contact", recipe.Name)
		}
		action["lines"] = []map[string]any{{
			"valueExtractor": "fee",
			"assetExtractor": "COIN",
			"categoryId":     plan.FeeCategoryID,
			"contactId":      plan.FeeContactID,
			"metadataIds":    []string{},
		}}
		action["ignoreFailPricing"] = plan.IgnoreFailPricing
	case "ignore-blank":
		// Ignore has no accounting action fields.
	}
	if recipe.Name == "metadata-categorization" && len(plan.Metadata) == 0 {
		return nil, fmt.Errorf("preset metadata-categorization requires at least one metadata key/value pair")
	}

	transfer := map[string]any{
		"name": plan.Name, "priority": plan.Priority, "disabled": !plan.Enabled,
		"accountingConnectionId": plan.AccountingConnectionID, "action": action,
		"direction": direction, "multiToken": multiToken, "autoCategorizeFee": autoCategorizeFee,
		"allowMismatch": allowMismatch, "collapseValues": false,
	}
	optionalString(transfer, "coin", plan.Asset)
	optionalString(transfer, "methodId", strings.TrimSpace(plan.MethodID))
	optionalString(transfer, "walletId", plan.WalletID)
	optionalString(transfer, "fromAddress", strings.TrimSpace(plan.FromAddress))
	optionalString(transfer, "toAddress", strings.TrimSpace(plan.ToAddress))
	if plan.AfterDateSEC > 0 {
		transfer["afterDateSEC"] = plan.AfterDateSEC
	}
	if plan.BeforeDateSEC > 0 {
		transfer["beforeDateSEC"] = plan.BeforeDateSEC
	}
	if len(plan.Metadata) > 0 {
		operator := strings.ToUpper(strings.TrimSpace(plan.MetadataOperator))
		if operator == "" {
			operator = "AND"
		}
		if operator != "AND" && operator != "OR" && operator != "NAND" && operator != "NOR" && operator != "XOR" {
			return nil, fmt.Errorf("unsupported metadata operator %q", operator)
		}
		pairs := make([]MetadataPair, 0, len(plan.Metadata))
		for _, pair := range plan.Metadata {
			pair.Key = strings.TrimSpace(pair.Key)
			pair.Value = strings.TrimSpace(pair.Value)
			if pair.Key == "" {
				return nil, fmt.Errorf("metadata key cannot be empty")
			}
			pairs = append(pairs, pair)
		}
		transfer["metadataRule"] = map[string]any{
			"operator": operator, "metadata": pairs, "txnRecordRule": plan.MetadataTransactionRecord,
		}
	}
	data, err := json.Marshal(map[string]any{"transfer": transfer})
	return json.RawMessage(data), err
}

func optionalString(dst map[string]any, key, value string) {
	if value != "" {
		dst[key] = value
	}
}
