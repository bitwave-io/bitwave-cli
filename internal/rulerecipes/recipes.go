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
	SchemaVersion = "1"
	SourceURL     = "https://docs.bitwave.io/docs/set-up-categorization-rules"
	LastVerified  = "2026-08-11"
)

type Field struct {
	Name        string `json:"name"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description"`
}

type Recipe struct {
	Name             string         `json:"name"`
	Summary          string         `json:"summary"`
	ActionType       string         `json:"actionType"`
	DefaultDirection string         `json:"defaultDirection"`
	DefaultMulti     bool           `json:"defaultMultiToken"`
	ApplySupported   bool           `json:"applySupported"`
	Fields           []Field        `json:"fields"`
	Defaults         map[string]any `json:"defaults"`
	Guidance         []string       `json:"guidance"`
}

var catalog = []Recipe{
	{
		Name: "simple-inflow", Summary: "Categorize matching inbound transactions to one category and contact.",
		ActionType: "SimpleCategorization", DefaultDirection: "Inbound", ApplySupported: true,
		Fields:   []Field{{"category", true, "Category ID or exact name."}, {"contact", true, "Contact ID or exact name."}, {"asset", false, "Single asset required for the match."}},
		Defaults: map[string]any{"multiToken": false, "autoCategorizeFee": true, "allowMismatch": false},
		Guidance: []string{"Use multiToken=false when --asset identifies a single-token rule.", "Use from/to address filters only for primary transaction addresses, not token transfer lines."},
	},
	{
		Name: "simple-outflow", Summary: "Categorize matching outbound transactions to one category and contact.",
		ActionType: "SimpleCategorization", DefaultDirection: "Outbound", ApplySupported: true,
		Fields:   []Field{{"category", true, "Category ID or exact name."}, {"contact", true, "Contact ID or exact name."}, {"asset", false, "Single asset required for the match."}},
		Defaults: map[string]any{"multiToken": false, "autoCategorizeFee": true, "allowMismatch": false},
		Guidance: []string{"Use multiToken=false when --asset identifies a single-token rule.", "Specify a separate fee category/contact when fees should not use the primary categorization."},
	},
	{
		Name: "trade", Summary: "Categorize swap/trade transactions and their fee contact.",
		ActionType: "TradeCategorization", DefaultDirection: "All", DefaultMulti: true, ApplySupported: true,
		Fields:   []Field{{"feeContact", true, "Fee contact ID or exact name."}},
		Defaults: map[string]any{"multiToken": true, "autoCategorizeFee": false, "allowMismatch": true},
		Guidance: []string{"Trades use multiToken=true because Bitwave trades exchange one asset for one or more assets.", "Do not add a single --asset unless the rule is intentionally asset-specific."},
	},
	{
		Name: "internal-transfer", Summary: "Categorize wallet-to-wallet transfers and associated fees.",
		ActionType: "InternalTransferCategorization", DefaultDirection: "All", ApplySupported: true,
		Fields:   []Field{{"feeCategory", true, "Fee category ID or exact name."}, {"feeContact", true, "Fee contact ID or exact name."}},
		Defaults: map[string]any{"multiToken": false, "autoCategorizeFee": true, "allowMismatch": false},
		Guidance: []string{"Enable multi-token only when matching transfers contain more than one transferred asset.", "Use --wallet to constrain the rule when different wallets require different fee treatment."},
	},
	{
		Name: "gas-fee-only", Summary: "Categorize transactions containing only network/contract execution fees.",
		ActionType: "InternalTransferCategorization", DefaultDirection: "Empty", ApplySupported: true,
		Fields:   []Field{{"feeCategory", true, "Category used for the gas fee."}, {"feeContact", true, "Contact used for the gas fee."}},
		Defaults: map[string]any{"multiToken": false, "autoCategorizeFee": true, "allowMismatch": false},
		Guidance: []string{"Direction Empty is the deployed Bitwave condition for transactions without a primary fund flow.", "Leave wallet empty for org-wide gas treatment or create one rule per wallet."},
	},
	{
		Name: "ignore-blank", Summary: "Ignore transactions that contain no transferred value.",
		ActionType: "Ignore", DefaultDirection: "Empty", ApplySupported: true,
		Fields: []Field{}, Defaults: map[string]any{"multiToken": false, "autoCategorizeFee": false, "allowMismatch": false},
		Guidance: []string{"Use a bounded date window first because enabled rules also affect historical data.", "Do not use this preset for failed pricing or otherwise non-blank economic activity."},
	},
	{
		Name: "detailed-categorization", Summary: "Categorize token-level or multi-line transaction details using extractor lines.",
		ActionType: "DetailedCategorization", DefaultDirection: "All", ApplySupported: false,
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
	Preset                 string
	ID                     string
	Name                   string
	Priority               int
	AccountingConnectionID string
	CategoryID             string
	ContactID              string
	FeeCategoryID          string
	FeeContactID           string
	Asset                  string
	Direction              string
	WalletID               string
	FromAddress            string
	ToAddress              string
	AfterDateSEC           int64
	BeforeDateSEC          int64
	Enabled                bool
	MultiToken             *bool
	AutoCategorizeFee      *bool
	AllowMismatch          *bool
	IgnoreFailPricing      bool
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
	case "simple-inflow", "simple-outflow":
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
	case "internal-transfer", "gas-fee-only":
		if plan.FeeCategoryID == "" || plan.FeeContactID == "" {
			return nil, fmt.Errorf("preset %q requires fee category and fee contact", recipe.Name)
		}
		action["feeCategoryId"] = plan.FeeCategoryID
		action["feeContactId"] = plan.FeeContactID
		action["ignoreFailPricing"] = plan.IgnoreFailPricing
	case "ignore-blank":
		// Ignore has no accounting action fields.
	}

	transfer := map[string]any{
		"name": plan.Name, "priority": plan.Priority, "disabled": !plan.Enabled,
		"accountingConnectionId": plan.AccountingConnectionID, "action": action,
		"direction": direction, "multiToken": multiToken, "autoCategorizeFee": autoCategorizeFee,
		"allowMismatch": allowMismatch, "collapseValues": false,
	}
	optionalString(transfer, "id", plan.ID)
	optionalString(transfer, "coin", plan.Asset)
	optionalString(transfer, "walletId", plan.WalletID)
	optionalString(transfer, "fromAddress", strings.TrimSpace(plan.FromAddress))
	optionalString(transfer, "toAddress", strings.TrimSpace(plan.ToAddress))
	if plan.AfterDateSEC > 0 {
		transfer["afterDateSEC"] = plan.AfterDateSEC
	}
	if plan.BeforeDateSEC > 0 {
		transfer["beforeDateSEC"] = plan.BeforeDateSEC
	}
	data, err := json.Marshal(map[string]any{"transfer": transfer})
	return json.RawMessage(data), err
}

func optionalString(dst map[string]any, key, value string) {
	if value != "" {
		dst[key] = value
	}
}
