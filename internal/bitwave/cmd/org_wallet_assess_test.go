package cmd

import (
	"strings"
	"testing"

	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

func int64Pointer(value int64) *int64 { return &value }

func validBabelRule() orgreports.BabelRollupRule {
	return orgreports.BabelRollupRule{
		RuleName: "Hourly incoming", Classification: "incoming", FingerPrint: "incomingToken",
		RollupAction: "rollup", Cadence: "hour", RoundPeriod: "end-of-period",
	}
}

func TestVolumeReviewRequiredBeforeWalletCreation(t *testing.T) {
	input := orgWalletInput{Name: "Wallet", NetworkID: "eth"}
	err := validateWalletVolumeAndRollups(input)
	if err == nil || !strings.Contains(err.Error(), "volume review is required") {
		t.Fatalf("error = %v", err)
	}
}

func TestHighVolumeWalletRequiresBabelRules(t *testing.T) {
	input := orgWalletInput{
		Name: "High volume", NetworkID: "eth",
		VolumeReview: walletVolumeReview{Reviewed: true, EstimatedTransactions: int64Pointer(20_000_000), Source: "explorer", Evidence: "20m transactions"},
	}
	err := validateWalletVolumeAndRollups(input)
	if err == nil || !strings.Contains(err.Error(), "define Babel rollup rules") {
		t.Fatalf("error = %v", err)
	}
	input.BabelRollupRules = []orgreports.BabelRollupRule{validBabelRule()}
	if err := validateWalletVolumeAndRollups(input); err != nil {
		t.Fatalf("valid high-volume plan rejected: %v", err)
	}
	assessment := assessWalletVolume(input)
	if assessment.Decision != "ready" || assessment.Risk != "high" {
		t.Fatalf("assessment = %#v", assessment)
	}
}

func TestUnknownVolumeRequiresAcknowledgement(t *testing.T) {
	input := orgWalletInput{Name: "Unknown", NetworkID: "eth", VolumeReview: walletVolumeReview{Reviewed: true}}
	if err := validateWalletVolumeAndRollups(input); err == nil {
		t.Fatal("expected unknown-volume guard")
	}
	input.VolumeReview.AcknowledgeUnknown = true
	if err := validateWalletVolumeAndRollups(input); err != nil {
		t.Fatalf("acknowledged unknown volume rejected: %v", err)
	}
	if got := assessWalletVolume(input).Decision; got != "ready_with_unknown_volume_warning" {
		t.Fatalf("decision = %q", got)
	}
}

func TestSolanaValidatorUsesAutomaticRollups(t *testing.T) {
	input := orgWalletInput{
		Name: "Validator", NetworkID: "sol", SolanaValidator: true,
		VolumeReview: walletVolumeReview{Reviewed: true, EstimatedTransactions: int64Pointer(20_000_000), Source: "user", Evidence: "validator operator estimate"},
	}
	if err := validateWalletVolumeAndRollups(input); err != nil {
		t.Fatalf("Solana validator rejected: %v", err)
	}
	if got := assessWalletVolume(input).Decision; got != "ready_solana_validator_auto_rollup" {
		t.Fatalf("decision = %q", got)
	}
	input.BabelRollupRules = []orgreports.BabelRollupRule{validBabelRule()}
	if err := validateWalletVolumeAndRollups(input); err == nil || !strings.Contains(err.Error(), "automatically") {
		t.Fatalf("expected automatic-rollup caveat, got %v", err)
	}
}

func TestBabelRollupRuleValidation(t *testing.T) {
	rule := validBabelRule()
	if err := validateBabelRollupRule(rule); err != nil {
		t.Fatal(err)
	}
	rule.Classification = "has spaces"
	if err := validateBabelRollupRule(rule); err == nil {
		t.Fatal("expected invalid classification")
	}
	rule = validBabelRule()
	rule.FingerPrint = "madeUp"
	if err := validateBabelRollupRule(rule); err == nil {
		t.Fatal("expected invalid fingerprint")
	}
}

func TestWalletAssessmentPromptsCoverResearchAndSupport(t *testing.T) {
	prompts := walletVolumePrompts()
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		prompts[0]["question"].(string), prompts[len(prompts)-1]["question"].(string),
	}, " ")))
	if !strings.Contains(text, "how many") || !strings.Contains(text, "bitwave") {
		t.Fatalf("prompts = %#v", prompts)
	}
}
