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

func TestMissingVolumeReviewWarnsButDoesNotBlockWalletCreation(t *testing.T) {
	input := orgWalletInput{Name: "Wallet", NetworkID: "eth"}
	if err := validateWalletVolumeAndRollups(input); err != nil {
		t.Fatalf("advisory preflight blocked creation: %v", err)
	}
	assessment := assessWalletVolume(input)
	if assessment.Decision != "ready_with_volume_warning" || assessment.Blocking || assessment.InteractionRequired || len(assessment.Warnings) != 1 {
		t.Fatalf("assessment = %#v", assessment)
	}
}

func TestHighVolumeWalletWithoutBabelRulesWarnsButDoesNotBlock(t *testing.T) {
	input := orgWalletInput{
		Name: "High volume", NetworkID: "eth",
		VolumeReview: walletVolumeReview{Reviewed: true, EstimatedTransactions: int64Pointer(20_000_000), Source: "explorer", Evidence: "20m transactions"},
	}
	if err := validateWalletVolumeAndRollups(input); err != nil {
		t.Fatalf("advisory rollup warning blocked creation: %v", err)
	}
	assessment := assessWalletVolume(input)
	if assessment.Decision != "ready_with_rollup_warning" || len(assessment.Warnings) == 0 {
		t.Fatalf("assessment = %#v", assessment)
	}
	input.BabelRollupRules = []orgreports.BabelRollupRule{validBabelRule()}
	if err := validateWalletVolumeAndRollups(input); err != nil {
		t.Fatalf("valid high-volume plan rejected: %v", err)
	}
	assessment = assessWalletVolume(input)
	if assessment.Decision != "ready" || assessment.Risk != "high" {
		t.Fatalf("assessment = %#v", assessment)
	}
}

func TestUnknownVolumeAcknowledgementChangesDecisionButIsNotRequired(t *testing.T) {
	input := orgWalletInput{Name: "Unknown", NetworkID: "eth", VolumeReview: walletVolumeReview{Reviewed: true}}
	if err := validateWalletVolumeAndRollups(input); err != nil {
		t.Fatalf("unknown volume blocked creation: %v", err)
	}
	if got := assessWalletVolume(input).Decision; got != "ready_with_volume_warning" {
		t.Fatalf("decision = %q", got)
	}
	input.VolumeReview.AcknowledgeUnknown = true
	if err := validateWalletVolumeAndRollups(input); err != nil {
		t.Fatalf("acknowledged unknown volume rejected: %v", err)
	}
	if got := assessWalletVolume(input).Decision; got != "ready_with_unknown_volume_warning" {
		t.Fatalf("decision = %q", got)
	}
}

func TestEstimateProvenanceWarnsButDoesNotBlock(t *testing.T) {
	input := orgWalletInput{
		Name: "Estimated", NetworkID: "eth",
		VolumeReview: walletVolumeReview{Reviewed: true, EstimatedTransactions: int64Pointer(42_000)},
	}
	if err := validateWalletVolumeAndRollups(input); err != nil {
		t.Fatalf("missing estimate provenance blocked creation: %v", err)
	}
	assessment := assessWalletVolume(input)
	if assessment.Decision != "ready" || len(assessment.Warnings) == 0 {
		t.Fatalf("assessment = %#v", assessment)
	}
}

func TestVolumeRiskOverrideReasonIsRecommendedButNotRequired(t *testing.T) {
	input := orgWalletInput{
		Name: "High volume override", NetworkID: "eth",
		VolumeReview: walletVolumeReview{
			Reviewed: true, EstimatedTransactions: int64Pointer(20_000_000),
			Source: "explorer", Evidence: "20m transactions", OverrideRisk: true,
		},
	}
	if err := validateWalletVolumeAndRollups(input); err != nil {
		t.Fatalf("override without reason blocked creation: %v", err)
	}
	withoutReason := assessWalletVolume(input)
	if withoutReason.Decision != "ready_with_volume_risk_override" || len(withoutReason.Warnings) < 2 {
		t.Fatalf("assessment without reason = %#v", withoutReason)
	}

	input.VolumeReview.OverrideReason = "User requires full unrolled history for an approved migration test"
	if err := validateWalletVolumeAndRollups(input); err != nil {
		t.Fatalf("explicit override rejected: %v", err)
	}
	assessment := assessWalletVolume(input)
	if assessment.Decision != "ready_with_volume_risk_override" || !assessment.RiskOverride || assessment.OverrideReason == "" {
		t.Fatalf("assessment = %#v", assessment)
	}
}

func TestVolumeRiskOverrideAllowsUnknownVolume(t *testing.T) {
	input := orgWalletInput{
		Name: "Unknown override", NetworkID: "eth",
		VolumeReview: walletVolumeReview{Reviewed: true, OverrideRisk: true, OverrideReason: "User explicitly accepted unknown ingestion volume"},
	}
	if err := validateWalletVolumeAndRollups(input); err != nil {
		t.Fatalf("explicit unknown-volume override rejected: %v", err)
	}
	if got := assessWalletVolume(input).Decision; got != "ready_with_volume_risk_override" {
		t.Fatalf("decision = %q", got)
	}
}

func TestVolumeRiskOverrideDoesNotBypassInvalidConfiguration(t *testing.T) {
	baseReview := walletVolumeReview{
		Reviewed: true, EstimatedTransactions: int64Pointer(20_000_000), Source: "explorer",
		Evidence: "20m transactions", OverrideRisk: true, OverrideReason: "User accepted ingestion risk",
	}
	invalidRule := validBabelRule()
	invalidRule.FingerPrint = "madeUp"
	input := orgWalletInput{Name: "Invalid Babel", NetworkID: "eth", VolumeReview: baseReview, BabelRollupRules: []orgreports.BabelRollupRule{invalidRule}}
	if err := validateWalletVolumeAndRollups(input); err == nil || !strings.Contains(err.Error(), "unsupported fingerPrint") {
		t.Fatalf("override bypassed invalid Babel rule: %v", err)
	}

	input = orgWalletInput{Name: "Invalid validator", NetworkID: "sol", SolanaValidator: true, VolumeReview: baseReview, BabelRollupRules: []orgreports.BabelRollupRule{validBabelRule()}}
	if err := validateWalletVolumeAndRollups(input); err == nil || !strings.Contains(err.Error(), "automatically") {
		t.Fatalf("override bypassed Solana validator guard: %v", err)
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
	if !strings.Contains(text, "how many") || !strings.Contains(text, "explicitly") {
		t.Fatalf("prompts = %#v", prompts)
	}
}
