package rulerecipes

type AccountMapping struct {
	Account  string `json:"account"`
	Category string `json:"category"`
	Type     string `json:"type"`
}

type MetadataPattern struct {
	Key                string `json:"key"`
	Value              string `json:"value"`
	TransactionType    string `json:"transactionType,omitempty"`
	GenericCategory    string `json:"genericCategory"`
	SpecificCategory   string `json:"specificCategory"`
	InteractingAddress string `json:"interactingAddress"`
	Meaning            string `json:"meaning"`
}

// NetworkTerm translates network-specific vocabulary into the generic
// transaction evidence and CLI flags an agent can use.
type NetworkTerm struct {
	Term      string   `json:"term"`
	Meaning   string   `json:"meaning"`
	CLIFields []string `json:"cliFields"`
	Guidance  []string `json:"guidance"`
}

// AccountingDecision is decision support for an agent. It suggests an
// economic account class but never embeds an organization's account IDs.
type AccountingDecision struct {
	Name                  string   `json:"name"`
	Ownership             string   `json:"ownership"`
	Direction             string   `json:"direction"`
	CounterpartyPattern   string   `json:"counterpartyPattern,omitempty"`
	MetadataKeys          []string `json:"metadataKeys,omitempty"`
	SuggestedAccountClass string   `json:"suggestedAccountClass"`
	SuggestedAction       string   `json:"suggestedAction"`
	CategoryContactNeeded bool     `json:"categoryContactNeeded"`
	Guidance              []string `json:"guidance"`
}

// RuleArchetype describes a reusable decision pattern, not a client rule.
// Category and contact IDs must always be resolved from the active org.
type RuleArchetype struct {
	Name              string   `json:"name"`
	TransactionType   string   `json:"transactionType"`
	Direction         string   `json:"direction"`
	DefaultScope      string   `json:"defaultScope"`
	MetadataKeys      []string `json:"metadataKeys,omitempty"`
	Action            string   `json:"action"`
	CategoryClass     string   `json:"categoryClass,omitempty"`
	ContactRequired   bool     `json:"contactRequired"`
	AutoCategorizeFee bool     `json:"autoCategorizeFee"`
	Guidance          []string `json:"guidance"`
}

type MetadataKnowledge struct {
	Source                 Source               `json:"source"`
	MethodIDSource         Source               `json:"methodIdSource"`
	Applicability          string               `json:"applicability"`
	Recommendation         []string             `json:"recommendation"`
	CandidateConditions    []string             `json:"candidateConditions"`
	MethodIDGuidance       []string             `json:"methodIdGuidance"`
	ExampleNetwork         string               `json:"exampleNetwork"`
	Operators              []string             `json:"operators"`
	DocumentedKeys         []string             `json:"documentedKeys"`
	NetworkTerminology     []NetworkTerm        `json:"networkTerminology"`
	AccountingDecisions    []AccountingDecision `json:"accountingDecisions"`
	StandardChart          []AccountMapping     `json:"standardChart"`
	StandardSpecificChart  []AccountMapping     `json:"standardSpecificChart"`
	GeneralPatterns        []MetadataPattern    `json:"generalPatterns"`
	RuleArchetypes         []RuleArchetype      `json:"ruleArchetypes"`
	AccountGuidance        []string             `json:"accountGuidance"`
	DataQualityChecks      []string             `json:"dataQualityChecks"`
	VendorSpecificGuidance []string             `json:"vendorSpecificGuidance"`
	InternalTransferStatus string               `json:"internalTransferStatus"`
}

func MetadataGuide() MetadataKnowledge {
	return MetadataKnowledge{
		Source:         Source{Title: "Metadata Based Rule Categorization", URL: MetadataSourceURL},
		MethodIDSource: Source{Title: "How to Use Rules", URL: RuleUsageSourceURL},
		Applicability:  "Metadata-based and method-ID conditions are general Bitwave rule capabilities. The fixed mappings below are Canton examples, not a restriction on other clients or networks.",
		Recommendation: []string{
			"Inspect representative transaction data before proposing a rule.",
			"Prefer stable, repeated, semantically meaningful metadata key/value pairs when available.",
			"Prefer methodId for repeated smart-contract calls when it separates the intended activity.",
			"Combine metadata or methodId with wallet, address, direction, or asset only when needed to prevent overmatching.",
			"Preview and validate against known matching and non-matching transactions before enabling a broad rule.",
		},
		CandidateConditions: []string{"metadata", "methodId", "wallet", "fromAddress", "toAddress", "direction", "coin"},
		MethodIDGuidance: []string{
			"methodId is a first-class Bitwave rule condition and is especially useful for repeated DeFi or smart-contract interactions.",
			"Read methodId from sampled transaction data; do not ask the user to know it in advance.",
			"The same method can have different accounting meaning across contracts or wallets, so add a stable narrowing condition when the sample shows ambiguity.",
		},
		ExampleNetwork: "Canton",
		Operators:      []string{"AND", "OR", "NAND", "NOR", "XOR"},
		DocumentedKeys: []string{"FeeType", "RewardFeeType", "RewardType", "TransactionType"},
		NetworkTerminology: []NetworkTerm{
			{
				Term: "partyId", Meaning: "Canton counterparty identifier shown on transaction lines; it serves the same rule-scoping role as a full blockchain address.",
				CLIFields: []string{"fromAddress", "toAddress"},
				Guidance:  []string{"Copy the complete exact party ID from transaction evidence.", "Use --from-address for inbound counterparty matching and --to-address for outbound counterparty matching.", "Never abbreviate, lowercase, or otherwise normalize a party ID."},
			},
			{
				Term: "0x0", Meaning: "Canton system-side party pattern used in the documented reward and network-fee flows.",
				CLIFields: []string{"fromAddress", "toAddress", "direction"},
				Guidance:  []string{"Inbound from 0x0 is reward evidence; inspect RewardType before selecting the revenue account.", "Outbound to 0x0 is fee evidence; inspect FeeType and RewardFeeType before selecting the expense account."},
			},
		},
		AccountingDecisions: []AccountingDecision{
			{
				Name: "owned-party-sweep", Ownership: "both-sides-owned", Direction: "All", SuggestedAccountClass: "internal transfer", SuggestedAction: "InternalTransferCategorization", CategoryContactNeeded: false,
				Guidance: []string{"Movement between party IDs or wallets owned by the same entity is not revenue or expense.", "Confirm ownership rather than inferring it from direction or wallet name."},
			},
			{
				Name: "canton-reward", Ownership: "inbound-external-or-system", Direction: "Inbound", CounterpartyPattern: "from partyId 0x0", MetadataKeys: []string{"RewardType", "TransactionType"}, SuggestedAccountClass: "reward or revenue", SuggestedAction: "Categorize", CategoryContactNeeded: true,
				Guidance: []string{"Use RewardType to distinguish application, validator, super-validator, and other reward activity.", "Recommend the closest approved reward/revenue account in the active organization and explain the metadata evidence."},
			},
			{
				Name: "canton-network-fee", Ownership: "outbound-system", Direction: "Outbound", CounterpartyPattern: "to partyId 0x0", MetadataKeys: []string{"FeeType", "TransactionType"}, SuggestedAccountClass: "network fee expense", SuggestedAction: "Categorize", CategoryContactNeeded: true,
				Guidance: []string{"Use FeeType for a more specific fee account when the approved chart supports it.", "Otherwise recommend the organization's general Canton or network-fee expense account."},
			},
			{
				Name: "canton-reward-claim-fee", Ownership: "outbound-system", Direction: "Outbound", MetadataKeys: []string{"RewardFeeType", "TransactionType"}, SuggestedAccountClass: "minting or reward-claim fee expense", SuggestedAction: "Categorize", CategoryContactNeeded: true,
				Guidance: []string{"RewardFeeType=sender_change_fee is separate from an ordinary network fee in the documented Canton model.", "Recommend a distinct approved reward-claim or minting-fee account when one exists."},
			},
			{
				Name: "application-subscription", Ownership: "outbound-external", Direction: "Outbound", CounterpartyPattern: "to known application partyId", SuggestedAccountClass: "subscription or application fee expense", SuggestedAction: "Categorize", CategoryContactNeeded: true,
				Guidance: []string{"Use the complete known party ID and wallet scope.", "Confirm the business relationship before selecting the account and contact."},
			},
			{
				Name: "application-rebate-or-revenue-share", Ownership: "inbound-external", Direction: "Inbound", CounterpartyPattern: "from known application partyId", SuggestedAccountClass: "rebate or revenue-share income", SuggestedAction: "Categorize", CategoryContactNeeded: true,
				Guidance: []string{"Use the complete known party ID and wallet scope.", "Direction alone does not distinguish rebate from revenue share; use business context and the approved chart."},
			},
			{
				Name: "unclear-canton-activity", Ownership: "unknown", Direction: "All", SuggestedAccountClass: "needs review", SuggestedAction: "NeedsReview", CategoryContactNeeded: false,
				Guidance: []string{"Do not force a category when party ownership, metadata, or business purpose is unclear.", "Inspect the transaction UI evidence or ask the user for the missing business context."},
			},
		},
		StandardChart: []AccountMapping{
			{"400", "Application Rewards", "Revenue"},
			{"401", "Super Validator Rewards", "Revenue"},
			{"402", "Validator Rewards", "Revenue"},
			{"403", "Application Revenue Share Reward", "Revenue"},
			{"404", "Application Subscription Revenue", "Revenue"},
			{"500", "Canton Coin Fees", "Expense"},
			{"501", "Canton Coin Reward Fee", "Expense"},
			{"502", "Application Subscription Fee", "Expense"},
			{"503", "Application Revenue Share Fee", "Expense"},
		},
		StandardSpecificChart: []AccountMapping{
			{"400", "Application Interaction Rewards", "Revenue"},
			{"401", "Super Validator Rewards", "Revenue"},
			{"402", "Transaction Validation Rewards", "Revenue"},
			{"403", "Validator Rewards", "Revenue"},
			{"404", "Application Revenue Share Reward", "Revenue"},
			{"405", "Application Subscription Revenue", "Revenue"},
			{"500", "Coin Locking Fee", "Expense"},
			{"501", "Contract Record Creation Fee", "Expense"},
			{"502", "Fixed Transfer Processing Fee", "Expense"},
			{"503", "Idle Coin Transfer Fee", "Expense"},
			{"504", "Idle Coin Usage Fee", "Expense"},
			{"505", "Locked Coin Transfer Fee", "Expense"},
			{"506", "Network Service Purchase Fee", "Expense"},
			{"507", "Reward Claim Balance Adjustment Fee", "Expense"},
			{"508", "Tiered Transfer Fee", "Expense"},
			{"509", "Transfer Balance Adjustment Fee", "Expense"},
			{"510", "Variable Transfer Processing Fee", "Expense"},
			{"511", "Application Subscription Fee", "Expense"},
			{"512", "Application Revenue Share Fee", "Expense"},
		},
		GeneralPatterns: []MetadataPattern{
			{Key: "FeeType", Value: "receiver_lock_holding_fee", TransactionType: "AmuletRules_Transfer", GenericCategory: "Canton Coin Fees", SpecificCategory: "Coin Locking Fee", InteractingAddress: "0x0", Meaning: "Receiver-side fee for holding a conditionally locked coin."},
			{Key: "FeeType", Value: "receiver_base_transfer_fee", TransactionType: "AmuletRules_Transfer", GenericCategory: "Canton Coin Fees", SpecificCategory: "Contract Record Creation Fee", InteractingAddress: "0x0", Meaning: "Receiver-side base fee for new coin contract records."},
			{Key: "FeeType", Value: "receiver_transfer_fee", TransactionType: "AmuletRules_Transfer", GenericCategory: "Canton Coin Fees", SpecificCategory: "Tiered Transfer Fee", InteractingAddress: "0x0", Meaning: "Tiered receiver transfer charge."},
			{Key: "FeeType", Value: "sender_base_transfer_fee", TransactionType: "AmuletRules_Transfer", GenericCategory: "Canton Coin Fees", SpecificCategory: "Fixed Transfer Processing Fee", InteractingAddress: "0x0", Meaning: "Fixed sender-side transfer processing charge."},
			{Key: "FeeType", Value: "sender_lock_holding_fee", TransactionType: "AmuletRules_Transfer", GenericCategory: "Canton Coin Fees", SpecificCategory: "Locked Coin Transfer Fee", InteractingAddress: "0x0", Meaning: "Sender-side cost of transferring locked coins."},
			{Key: "FeeType", Value: "holding_fees", TransactionType: "AmuletRules_Transfer", GenericCategory: "Canton Coin Fees", SpecificCategory: "Idle Coin Transfer Fee", InteractingAddress: "0x0", Meaning: "Holding fee consumed during a standard transfer."},
			{Key: "FeeType", Value: "holding_fees", TransactionType: "AmuletRules_BuyMemberTraffic", GenericCategory: "Canton Coin Fees", SpecificCategory: "Idle Coin Usage Fee", InteractingAddress: "0x0", Meaning: "Holding fee consumed during network traffic or service usage."},
			{Key: "FeeType", Value: "sender_fee", TransactionType: "AmuletRules_BuyMemberTraffic", GenericCategory: "Canton Coin Fees", SpecificCategory: "Network Service Purchase Fee", InteractingAddress: "0x0", Meaning: "Sender cost for purchasing network traffic or services."},
			{Key: "FeeType", Value: "sender_change_fee", TransactionType: "AmuletRules_Transfer", GenericCategory: "Canton Coin Fees", SpecificCategory: "Transfer Balance Adjustment Fee", InteractingAddress: "0x0", Meaning: "Fee for recording sender change after a transfer."},
			{Key: "FeeType", Value: "sender_transfer_fee", TransactionType: "AmuletRules_Transfer", GenericCategory: "Canton Coin Fees", SpecificCategory: "Variable Transfer Processing Fee", InteractingAddress: "0x0", Meaning: "Variable sender-side transfer charge."},
			{Key: "RewardFeeType", Value: "sender_change_fee", TransactionType: "AmuletRules_Transfer", GenericCategory: "Canton Coin Reward Fee", SpecificCategory: "Reward Claim Balance Adjustment Fee", InteractingAddress: "0x0", Meaning: "Balance adjustment fee when claiming a reward during a transfer."},
			{Key: "RewardFeeType", Value: "sender_change_fee", TransactionType: "AmuletRules_BuyMemberTraffic", GenericCategory: "Canton Coin Reward Fee", SpecificCategory: "Transfer Balance Adjustment Fee", InteractingAddress: "0x0", Meaning: "Balance adjustment fee when claiming a reward during network-service usage."},
			{Key: "RewardType", Value: "input_app_reward_amount", TransactionType: "AmuletRules_Transfer", GenericCategory: "Application Rewards", SpecificCategory: "Application Interaction Rewards", InteractingAddress: "0x0", Meaning: "Reward for application interaction."},
			{Key: "RewardType", Value: "input_sv_reward_amount", TransactionType: "AmuletRules_Transfer", GenericCategory: "Super Validator Rewards", SpecificCategory: "Super Validator Rewards", InteractingAddress: "0x0", Meaning: "Super validator participation reward."},
			{Key: "RewardType", Value: "input_validator_reward_amount", TransactionType: "AmuletRules_Transfer", GenericCategory: "Validator Rewards", SpecificCategory: "Transaction Validation Rewards", InteractingAddress: "0x0", Meaning: "Validator reward associated with a transfer."},
			{Key: "RewardType", Value: "input_validator_reward_amount", TransactionType: "AmuletRules_BuyMemberTraffic", GenericCategory: "Validator Rewards", SpecificCategory: "Validator Rewards", InteractingAddress: "0x0", Meaning: "Validator reward associated with network traffic or service usage."},
		},
		RuleArchetypes: []RuleArchetype{
			{
				Name: "canton-network-fee", TransactionType: "Standard", Direction: "Outbound", DefaultScope: "wallet",
				MetadataKeys: []string{"TransactionType", "FeeType"}, Action: "SimpleCategorization", CategoryClass: "expense", ContactRequired: true, AutoCategorizeFee: false,
				Guidance: []string{"Match the exact observed FeeType and, when needed, TransactionType.", "Use a general network-fee account or a more specific fee account only after the user approves the COA treatment."},
			},
			{
				Name: "canton-reward-claim-fee", TransactionType: "Standard", Direction: "Outbound", DefaultScope: "wallet",
				MetadataKeys: []string{"TransactionType", "RewardFeeType"}, Action: "SimpleCategorization", CategoryClass: "expense", ContactRequired: true, AutoCategorizeFee: false,
				Guidance: []string{"RewardFeeType is distinct from FeeType; do not merge the two metadata fields.", "Resolve the client-approved reward or minting fee account and contact from the active org."},
			},
			{
				Name: "canton-reward", TransactionType: "Standard", Direction: "Inbound", DefaultScope: "wallet",
				MetadataKeys: []string{"TransactionType", "RewardType"}, Action: "SimpleCategorization", CategoryClass: "revenue", ContactRequired: true, AutoCategorizeFee: true,
				Guidance: []string{"Separate application, validator, and super-validator reward types when the observed metadata supports that distinction.", "Wallet purpose can change the accounting treatment, so do not generalize across wallets without evidence."},
			},
			{
				Name: "canton-wallet-program-activity", TransactionType: "Standard", Direction: "All", DefaultScope: "wallet",
				Action: "SimpleCategorization", CategoryClass: "client-specific", ContactRequired: true, AutoCategorizeFee: true,
				Guidance: []string{"Rebates, subscriptions, and other program activity require transaction evidence beyond a wallet name before creating a broad rule.", "Prefer stable metadata or counterparty evidence; a wallet-only rule is an explicit, validated exception."},
			},
			{
				Name: "canton-sweep", TransactionType: "InternalTransfer", Direction: "All", DefaultScope: "organization",
				Action: "InternalTransferCategorization", ContactRequired: false, AutoCategorizeFee: true,
				Guidance: []string{"Treat confirmed sweeps between owned wallets as internal transfers, not revenue or expense.", "Use one organization-wide internal-transfer rule by default; add wallet scope only when the observed sweep behavior requires an explicit exception."},
			},
		},
		AccountGuidance: []string{
			"Treat example account names and numbers as mappings, not universal defaults. Resolve categories from the active organization's approved chart of accounts.",
			"Use transaction metadata to recommend the closest economic account class and explain why; do not merely return raw metadata for the user to interpret.",
			"Resolve real party IDs only from the active organization's transaction evidence. Never embed client or vendor party IDs in reusable CLI knowledge.",
			"A generic Canton fee expense account is acceptable when the client has not approved separate accounts for holding, transfer, locking, traffic, or reward-claim fees.",
			"Reward, rebate, subscription revenue, subscription expense, and minting-fee treatments are client accounting choices; never infer them from a wallet name alone.",
			"Simple categorization requires both a category and contact. Internal transfers do not require either, but their fee mapping still requires the applicable fee category/contact.",
		},
		DataQualityChecks: []string{
			"Read metadata from representative uncategorized transactions and preserve exact case, punctuation, underscores, and hyphens.",
			"Treat sender_change_fee and sender-change_fee as different values until live transaction evidence proves normalization is safe.",
			"Do not use EventId, RootTxn, transaction hashes, timestamps, block numbers, or other unique values as recurring rule conditions.",
			"Validate each proposed rule against at least one expected match and one expected non-match in the same wallet before enabling it.",
			"Detect overlap between wallet-wide standard rules and metadata-specific rules; the more specific rule must run first.",
		},
		VendorSpecificGuidance: []string{
			"TransactionType values such as AmuletRules_Transfer and AmuletRules_BuyMemberTraffic are network-specific and are not sufficient by themselves to choose an accounting category.",
			"Combine TransactionType with the interacting vendor address using --from-address or --to-address.",
			"The documented treatments include application revenue share rewards and application subscription fees; the user must select the treatment appropriate to that vendor relationship.",
		},
		InternalTransferStatus: "Do not invent a metadata mapping for internal transfers. Confirmed Canton sweeps use the InternalTransferCategorization action; keep the default organization-wide rule unless transaction evidence requires a wallet-scoped exception.",
	}
}
