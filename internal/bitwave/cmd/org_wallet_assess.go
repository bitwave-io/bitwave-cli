package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

const highVolumeWalletThreshold int64 = 1_000_000

type walletVolumeReview struct {
	Reviewed              bool   `json:"reviewed"`
	EstimatedTransactions *int64 `json:"estimatedTransactions,omitempty"`
	Source                string `json:"source,omitempty"`
	Evidence              string `json:"evidence,omitempty"`
	AcknowledgeUnknown    bool   `json:"acknowledgeUnknown,omitempty"`
}

type walletVolumeAssessment struct {
	Wallet                string   `json:"wallet"`
	NetworkID             string   `json:"networkId"`
	Risk                  string   `json:"risk"`
	Decision              string   `json:"decision"`
	EstimatedTransactions *int64   `json:"estimatedTransactions,omitempty"`
	HighVolumeThreshold   int64    `json:"highVolumeThreshold"`
	BabelRollupRuleCount  int      `json:"babelRollupRuleCount"`
	SolanaValidator       bool     `json:"solanaValidator"`
	Questions             []string `json:"questions,omitempty"`
	Recommendations       []string `json:"recommendations"`
}

func newOrgWalletsAssessCmd() *cobra.Command {
	var input string
	cmd := &cobra.Command{
		Use:   "assess",
		Short: "Assess wallet transaction volume and rollup readiness before creation",
		Long: `Produce an LLM-friendly preflight without changing Bitwave.

The LLM should ask the user about expected transaction volume and, when
possible, research the address with a block explorer or network API. Record the
estimated transactions for the requested sync window, the source, and evidence
in volumeReview. Unknown or high volume is never silently treated as safe.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(input) == "" {
				return errors.New("--input is required")
			}
			inputs, err := loadOrgWalletInputs(orgWalletAddFlags{input: input}, cmd.InOrStdin())
			if err != nil {
				return err
			}
			assessments := make([]walletVolumeAssessment, 0, len(inputs))
			for i := range inputs {
				if err := normalizeAndValidateOrgWallet(&inputs[i]); err != nil {
					return fmt.Errorf("wallet %d: %w", i+1, err)
				}
				assessments = append(assessments, assessWalletVolume(inputs[i]))
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{
				"schemaVersion":         "1",
				"highVolumeThreshold":   highVolumeWalletThreshold,
				"thresholdNote":         "Operational CLI heuristic, not a published Bitwave platform limit.",
				"prompts":               walletVolumePrompts(),
				"assessments":           assessments,
				"supportRecommendation": "If transaction volume or an appropriate rollup design is unclear, speak with Bitwave before adding the wallet.",
			})
		},
	}
	cmd.Flags().StringVarP(&input, "input", "i", "", "Wallet JSON file, or - for stdin (required)")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func walletVolumePrompts() []map[string]any {
	return []map[string]any{
		{"id": "expected_volume", "question": "Approximately how many historical transactions will this wallet import for the selected sync window?", "responseField": "volumeReview.estimatedTransactions", "suggestedResearch": []string{"ask the user", "block explorer transaction count", "network indexer or API"}},
		{"id": "wallet_usage", "question": "Is this a high-volume payments, exchange, NFT, contract, staking, or validator wallet?", "responseField": "volumeReview.evidence"},
		{"id": "sync_window", "question": "Is full history required, or can syncStartDateSEC reduce the import window?", "responseField": "syncStartDateSEC"},
		{"id": "rollup_design", "question": "Which transaction patterns should be rolled up, at what cadence, and which dimensions must stay separate?", "responseField": "babelRollupRules"},
		{"id": "uncertainty", "question": "If volume or rollup design is uncertain, has the user acknowledged the risk and the recommendation to speak with Bitwave before ingestion?", "responseField": "volumeReview.acknowledgeUnknown"},
	}
}

func assessWalletVolume(input orgWalletInput) walletVolumeAssessment {
	a := walletVolumeAssessment{
		Wallet: input.Name, NetworkID: input.NetworkID, Risk: "unknown", Decision: "needs_user_input",
		EstimatedTransactions: input.VolumeReview.EstimatedTransactions, HighVolumeThreshold: highVolumeWalletThreshold,
		BabelRollupRuleCount: len(input.BabelRollupRules), SolanaValidator: input.SolanaValidator,
		Recommendations: []string{},
	}
	if !input.VolumeReview.Reviewed {
		a.Questions = append(a.Questions, "Confirm that wallet volume was reviewed with the user before creation.")
	}
	if input.VolumeReview.EstimatedTransactions == nil {
		a.Questions = append(a.Questions, "Provide an estimated transaction count or explicitly acknowledge that volume is unknown.")
		a.Recommendations = append(a.Recommendations, "Research the address using a block explorer or network API and speak with Bitwave if volume remains unknown.")
		if input.VolumeReview.Reviewed && input.VolumeReview.AcknowledgeUnknown {
			a.Decision = "ready_with_unknown_volume_warning"
		}
		return a
	}
	count := *input.VolumeReview.EstimatedTransactions
	switch {
	case count >= highVolumeWalletThreshold:
		a.Risk = "high"
	case count >= 100_000:
		a.Risk = "elevated"
	default:
		a.Risk = "standard"
	}
	if input.SolanaValidator {
		if input.NetworkID != "sol" {
			a.Decision = "invalid_solana_validator_network"
			a.Recommendations = append(a.Recommendations, "Solana validator mode is valid only for networkId sol.")
			return a
		}
		if input.VolumeReview.Reviewed {
			a.Decision = "ready_solana_validator_auto_rollup"
		}
		a.Recommendations = append(a.Recommendations, "Do not add Babel rules: Solana validator transactions are rolled up automatically.")
		return a
	}
	if count >= highVolumeWalletThreshold && len(input.BabelRollupRules) == 0 {
		a.Decision = "needs_babel_rollups"
		a.Questions = append(a.Questions, "Define Babel rollup rules before creating this high-volume wallet.")
		a.Recommendations = append(a.Recommendations, "Choose fingerprints, cadence, handling, and separation dimensions; speak with Bitwave if unsure.")
		return a
	}
	if input.VolumeReview.Reviewed {
		a.Decision = "ready"
	}
	if a.Risk == "high" {
		a.Recommendations = append(a.Recommendations, "Review the proposed Babel rules with Bitwave for very large imports.")
	}
	return a
}

func validateWalletVolumeAndRollups(input orgWalletInput) error {
	if !input.VolumeReview.Reviewed {
		return errors.New("volume review is required before wallet creation; run `bitwave org wallets assess --input ...` and set volumeReview.reviewed=true")
	}
	if input.VolumeReview.EstimatedTransactions == nil && !input.VolumeReview.AcknowledgeUnknown {
		return errors.New("transaction volume is unknown; provide volumeReview.estimatedTransactions or set acknowledgeUnknown=true after discussing the risk (speak with Bitwave if unsure)")
	}
	if input.VolumeReview.EstimatedTransactions != nil && *input.VolumeReview.EstimatedTransactions < 0 {
		return errors.New("volumeReview.estimatedTransactions cannot be negative")
	}
	if input.VolumeReview.EstimatedTransactions != nil && strings.TrimSpace(input.VolumeReview.Source) == "" {
		return errors.New("volumeReview.source is required when an estimated transaction count is provided")
	}
	if input.VolumeReview.EstimatedTransactions != nil && strings.TrimSpace(input.VolumeReview.Evidence) == "" {
		return errors.New("volumeReview.evidence is required when an estimated transaction count is provided")
	}
	if input.SolanaValidator {
		if input.NetworkID != "sol" {
			return errors.New("solanaValidator is valid only for networkId sol")
		}
		if len(input.BabelRollupRules) > 0 {
			return errors.New("Solana validator transactions roll up automatically; do not configure Babel rollup rules")
		}
	}
	for i, rule := range input.BabelRollupRules {
		if err := validateBabelRollupRule(rule); err != nil {
			return fmt.Errorf("Babel rollup rule %d: %w", i+1, err)
		}
	}
	if !input.SolanaValidator && input.VolumeReview.EstimatedTransactions != nil && *input.VolumeReview.EstimatedTransactions >= highVolumeWalletThreshold && len(input.BabelRollupRules) == 0 {
		return fmt.Errorf("estimated volume is %d transactions; define Babel rollup rules before creation or speak with Bitwave", *input.VolumeReview.EstimatedTransactions)
	}
	return nil
}

func validateBabelRollupRule(rule orgreports.BabelRollupRule) error {
	if strings.TrimSpace(rule.RuleName) == "" {
		return errors.New("ruleName is required")
	}
	if strings.TrimSpace(rule.Classification) == "" {
		return errors.New("classification is required")
	}
	for _, r := range rule.Classification {
		if !(r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return errors.New("classification may contain only letters, numbers, underscores, and dashes")
		}
	}
	if !stringIn(rule.FingerPrint, "incomingNative", "incomingToken", "outgoingNative", "outgoingToken", "onlyFee", "errored", "any", "simpleIncoming", "simpleOutgoing", "simpleTrade") {
		return fmt.Errorf("unsupported fingerPrint %q", rule.FingerPrint)
	}
	if !stringIn(rule.RollupAction, "rollup", "rollupFromTo", "rollupByAsset", "nonRollup", "ignore") {
		return fmt.Errorf("unsupported rollupAction %q", rule.RollupAction)
	}
	if !stringIn(rule.Cadence, "hour", "day", "month") {
		return fmt.Errorf("unsupported cadence %q", rule.Cadence)
	}
	if rule.SeparateByTrade != "" && !stringIn(rule.SeparateByTrade, "none", "assets", "direction") {
		return fmt.Errorf("unsupported separateByTrade %q", rule.SeparateByTrade)
	}
	if rule.RoundPeriod != "" && !stringIn(rule.RoundPeriod, "start-of-period", "end-of-period") {
		return fmt.Errorf("unsupported roundPeriod %q", rule.RoundPeriod)
	}
	if rule.StartSEC != nil && rule.EndSEC != nil && *rule.StartSEC > *rule.EndSEC {
		return errors.New("startSec must be before endSec")
	}
	return nil
}

func stringIn(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}
