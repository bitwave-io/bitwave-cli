package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

func newLookupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lookup",
		Short: "Look up token, contract, block, and historical price data",
		Long: `Query Bitwave's public address and pricing services. These lookups do not
require an active organization and always return structured JSON.`,
	}
	cmd.AddCommand(
		newTokenLookupCmd(), newContractLookupCmd(), newActiveNetworksLookupCmd(),
		newBlockLookupCmd(), newBlockTimeLookupCmd(), newHistoricalPriceLookupCmd(),
	)
	return cmd
}

func publicLookupClient() *orgreports.Client {
	return orgreports.New(resolveCoreBaseURL(), func() (string, error) { return "", nil })
}

func newTokenLookupCmd() *cobra.Command {
	return &cobra.Command{
		Use: "token SYMBOL", Short: "Return metadata for a token symbol", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := publicLookupClient().PublicSymbol(cmd.Context(), strings.TrimSpace(args[0]))
			if err != nil {
				return fmt.Errorf("look up token %s: %w", args[0], err)
			}
			return writeLookupResult(cmd, "token", result)
		},
	}
}

func newActiveNetworksLookupCmd() *cobra.Command {
	return &cobra.Command{
		Use: "networks CONTRACT_ADDRESS", Aliases: []string{"ca"}, Short: "List active networks for a contract address", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := publicLookupClient().PublicActiveNetworks(cmd.Context(), strings.TrimSpace(args[0]))
			if err != nil {
				return fmt.Errorf("look up active contract networks: %w", err)
			}
			return writeLookupResult(cmd, "active-networks", result)
		},
	}
}

func newContractLookupCmd() *cobra.Command {
	return &cobra.Command{
		Use: "contract NETWORK_ID CONTRACT_ADDRESS", Short: "Return contract metadata on one network", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := publicLookupClient().PublicContract(cmd.Context(), strings.TrimSpace(args[0]), strings.TrimSpace(args[1]))
			if err != nil {
				return fmt.Errorf("look up contract: %w", err)
			}
			return writeLookupResult(cmd, "contract", result)
		},
	}
}

func newBlockLookupCmd() *cobra.Command {
	return &cobra.Command{
		Use: "block NETWORK_ID DATE", Short: "Return the block at a UTC date or timestamp", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			at, err := parseLookupTime(args[1], false)
			if err != nil {
				return err
			}
			result, err := publicLookupClient().PublicBlockAt(cmd.Context(), strings.TrimSpace(args[0]), at.Format(time.RFC3339))
			if err != nil {
				return fmt.Errorf("look up block: %w", err)
			}
			return writeLookupResult(cmd, "block", result)
		},
	}
}

func newBlockTimeLookupCmd() *cobra.Command {
	return &cobra.Command{
		Use: "block-time NETWORK_ID BLOCK_NUMBER", Aliases: []string{"blocktime"}, Short: "Return the UTC timestamp of a block", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := strconv.ParseUint(args[1], 10, 64); err != nil {
				return errors.New("BLOCK_NUMBER must be a non-negative integer")
			}
			result, err := publicLookupClient().PublicBlockTime(cmd.Context(), strings.TrimSpace(args[0]), args[1])
			if err != nil {
				return fmt.Errorf("look up block time: %w", err)
			}
			var value map[string]any
			if json.Unmarshal(result, &value) == nil {
				if sec, ok := numericInt64(value["timestampSEC"]); ok && sec > 0 {
					value["timestampISO"] = time.Unix(sec, 0).UTC().Format(time.RFC3339)
					return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "lookup": "block-time", "result": value})
				}
			}
			return writeLookupResult(cmd, "block-time", result)
		},
	}
}

func newHistoricalPriceLookupCmd() *cobra.Command {
	var at, fiat string
	cmd := &cobra.Command{
		Use: "price SYMBOL", Short: "Return a token's historical fiat price", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			when, err := parseLookupTime(at, true)
			if err != nil {
				return err
			}
			fiat = strings.ToUpper(strings.TrimSpace(fiat))
			if fiat == "" {
				return errors.New("--fiat is required")
			}
			result, err := publicLookupClient().PublicPrice(cmd.Context(), strings.ToUpper(strings.TrimSpace(args[0])), when.Unix(), fiat)
			if err != nil {
				return fmt.Errorf("look up historical price: %w", err)
			}
			return writeLookupResult(cmd, "price", result)
		},
	}
	cmd.Flags().StringVar(&at, "at", "", "UTC date (YYYY-MM-DD or MM-DD-YYYY) or RFC3339 timestamp (required)")
	cmd.Flags().StringVar(&fiat, "fiat", "USD", "Fiat quote currency")
	return cmd
}

func parseLookupTime(value string, required bool) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			return time.Time{}, errors.New("--at is required")
		}
		return time.Time{}, errors.New("DATE is required")
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02", "01-02-2006"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid date %q (use YYYY-MM-DD, MM-DD-YYYY, or RFC3339)", value)
}

func numericInt64(value any) (int64, bool) {
	switch item := value.(type) {
	case float64:
		return int64(item), true
	case json.Number:
		result, err := item.Int64()
		return result, err == nil
	case string:
		result, err := strconv.ParseInt(item, 10, 64)
		return result, err == nil
	default:
		return 0, false
	}
}

func writeLookupResult(cmd *cobra.Command, kind string, result json.RawMessage) error {
	return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "lookup": kind, "result": result})
}
