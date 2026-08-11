package rulerecipes

type AccountMapping struct {
	Account  string `json:"account"`
	Category string `json:"category"`
	Type     string `json:"type"`
}

type MetadataPattern struct {
	Key                string `json:"key"`
	Value              string `json:"value"`
	GenericCategory    string `json:"genericCategory"`
	SpecificCategory   string `json:"specificCategory"`
	InteractingAddress string `json:"interactingAddress"`
	Meaning            string `json:"meaning"`
}

type MetadataKnowledge struct {
	Source                 Source            `json:"source"`
	MethodIDSource         Source            `json:"methodIdSource"`
	Applicability          string            `json:"applicability"`
	Recommendation         []string          `json:"recommendation"`
	CandidateConditions    []string          `json:"candidateConditions"`
	MethodIDGuidance       []string          `json:"methodIdGuidance"`
	ExampleNetwork         string            `json:"exampleNetwork"`
	Operators              []string          `json:"operators"`
	DocumentedKeys         []string          `json:"documentedKeys"`
	StandardChart          []AccountMapping  `json:"standardChart"`
	StandardSpecificChart  []AccountMapping  `json:"standardSpecificChart"`
	GeneralPatterns        []MetadataPattern `json:"generalPatterns"`
	VendorSpecificGuidance []string          `json:"vendorSpecificGuidance"`
	InternalTransferStatus string            `json:"internalTransferStatus"`
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
			{"FeeType", "receiver_lock_holding_fee", "Canton Coin Fees", "Coin Locking Fee", "0x0", "Receiver-side fee for holding a conditionally locked coin."},
			{"FeeType", "receiver_base_transfer_fee", "Canton Coin Fees", "Contract Record Creation Fee", "0x0", "Receiver-side base fee for new coin contract records."},
			{"FeeType", "receiver_transfer_fee", "Canton Coin Fees", "Tiered Transfer Fee", "0x0", "Tiered receiver transfer charge."},
			{"FeeType", "sender_base_transfer_fee", "Canton Coin Fees", "Fixed Transfer Processing Fee", "0x0", "Fixed sender-side transfer processing charge."},
			{"FeeType", "sender_lock_holding_fee", "Canton Coin Fees", "Locked Coin Transfer Fee", "0x0", "Sender-side cost of transferring locked coins."},
			{"FeeType", "holding_fees", "Canton Coin Fees", "Idle Coin Transfer Fee", "0x0", "Holding fee consumed during network service usage."},
			{"FeeType", "sender_fee", "Canton Coin Fees", "Network Service Purchase Fee", "0x0", "Sender cost for purchasing network traffic or services."},
			{"FeeType", "sender_change_fee", "Canton Coin Fees", "Transfer Balance Adjustment Fee", "0x0", "Fee for recording sender change after a transfer."},
			{"FeeType", "sender_transfer_fee", "Canton Coin Fees", "Variable Transfer Processing Fee", "0x0", "Variable sender-side transfer charge."},
			{"RewardFeeType", "sender_change_fee", "Canton Coin Reward Fee", "Reward Claim Balance Adjustment Fee", "0x0", "Balance adjustment fee when claiming rewards."},
			{"RewardType", "input_app_reward_amount", "Application Rewards", "Application Interaction Rewards", "0x0", "Reward for application interaction."},
			{"RewardType", "input_sv_reward_amount", "Super Validator Rewards", "Super Validator Rewards", "0x0", "Super validator participation reward."},
			{"RewardType", "input_validator_reward_amount", "Validator Rewards", "Validator Rewards", "0x0", "Validator network-service reward."},
		},
		VendorSpecificGuidance: []string{
			"TransactionType=Amulet_Rules Transfer is vendor-specific and is not sufficient by itself to choose an accounting category.",
			"Combine TransactionType with the interacting vendor address using --from-address or --to-address.",
			"The documented treatments include application revenue share rewards and application subscription fees; the user must select the treatment appropriate to that vendor relationship.",
		},
		InternalTransferStatus: "The official metadata-rule guide marks its Internal Transfer Rules section as under construction; do not invent metadata mappings for it.",
	}
}
