package cmd

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/rulerecipes"
)

func newRuleMetadataGuideCmd() *cobra.Command {
	var key, value, chart string
	var limit int
	cmd := &cobra.Command{
		Use:   "metadata-guide",
		Short: "Explain general metadata/method-ID rule strategy and documented examples",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if limit < 1 || limit > 100 {
				return errors.New("--limit must be between 1 and 100")
			}
			chart = strings.ToLower(strings.TrimSpace(chart))
			if chart != "none" && chart != "both" && chart != "standard" && chart != "specific" {
				return errors.New("--chart must be none, standard, specific, or both")
			}
			guide := rulerecipes.MetadataGuide()
			patterns := make([]rulerecipes.MetadataPattern, 0)
			if chart != "none" || key != "" || value != "" {
				for _, pattern := range guide.GeneralPatterns {
					if key != "" && !strings.EqualFold(pattern.Key, key) {
						continue
					}
					if value != "" && !strings.Contains(strings.ToLower(pattern.Value), strings.ToLower(value)) {
						continue
					}
					patterns = append(patterns, pattern)
					if len(patterns) == limit {
						break
					}
				}
			}
			standard, specific := guide.StandardChart, guide.StandardSpecificChart
			if chart == "none" {
				standard, specific = nil, nil
			} else if chart == "standard" {
				specific = nil
			}
			if chart == "specific" {
				standard = nil
			}
			result := map[string]any{
				"schemaVersion": rulerecipes.SchemaVersion, "lastVerified": rulerecipes.LastVerified,
				"source": guide.Source, "methodIdSource": guide.MethodIDSource, "applicability": guide.Applicability, "recommendation": guide.Recommendation,
				"candidateConditions": guide.CandidateConditions, "methodIdGuidance": guide.MethodIDGuidance,
				"operators": guide.Operators, "filters": map[string]any{"key": key, "value": value, "chart": chart, "limit": limit},
			}
			if chart == "none" && key == "" && value == "" {
				result["documentedExamplesAvailable"] = map[string]any{"network": guide.ExampleNetwork, "charts": []string{"standard", "specific"}}
			} else {
				result["exampleNetwork"] = guide.ExampleNetwork
				result["documentedExampleKeys"] = guide.DocumentedKeys
				result["patterns"] = patterns
				result["vendorSpecificGuidance"] = guide.VendorSpecificGuidance
				result["internalTransferStatus"] = guide.InternalTransferStatus
				if standard != nil {
					result["standardChart"] = standard
				}
				if specific != nil {
					result["standardSpecificChart"] = specific
				}
			}
			return writeJSON(cmd.OutOrStdout(), result)
		},
	}
	cmd.Flags().StringVar(&key, "key", "", "Only mappings for one metadata key")
	cmd.Flags().StringVar(&value, "value", "", "Only mapping values containing this text")
	cmd.Flags().StringVar(&chart, "chart", "none", "Canton example chart: none, standard, specific, or both")
	cmd.Flags().IntVar(&limit, "limit", 25, "Maximum metadata patterns to return")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}
