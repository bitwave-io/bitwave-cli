package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

// This is the union of Bitwave's current Add Source catalog and the legacy
// wallet adapter. Creation remains forward-compatible: an unlisted canonical
// network id is passed to the API, which is the final authority.
var organizationWalletNetworks = map[string]string{
	"aleo": "Aleo", "alot": "Dexalot", "apt": "Aptos", "arb": "Arbitrum",
	"aurora": "Aurora", "avaxc": "Avalanche C-Chain", "avaxp": "Avalanche P-Chain",
	"base": "Base", "bch": "Bitcoin Cash", "beacon": "Beacon", "bsc": "BNB Chain",
	"btc": "Bitcoin", "canton": "Canton", "casper": "Casper", "celo": "Celo",
	"cosmos": "Cosmos", "dash": "Dash", "doge": "Dogecoin", "dot": "Polkadot",
	"eos": "EOS", "eth": "Ethereum", "fil": "Filecoin", "flow": "Flow",
	"ftm": "Fantom", "gnosis": "Gnosis", "hbar": "Hedera", "imx": "Immutable X",
	"kava": "Kava", "kaia": "Kaia", "klay": "Klaytn", "ksm": "Kusama",
	"ltc": "Litecoin", "mina": "Mina", "near": "NEAR", "op": "Optimism",
	"osmo": "Osmosis", "polygon": "Polygon", "polyx": "Polymesh", "rose": "Oasis",
	"sol": "Solana", "stx": "Stacks", "terra": "Terra", "xlm": "Stellar",
	"xrp": "XRP", "zec": "Zcash", "zeta": "ZetaChain",
}

var organizationWalletNetworkAliases = map[string]string{
	"aptos": "apt", "arbitrum": "arb", "avalanche": "avaxc", "avalanche-c": "avaxc",
	"avalanche-p": "avaxp", "bitcoin": "btc", "bitcoin-cash": "bch", "bnb": "bsc",
	"bnb-chain": "bsc", "dogecoin": "doge", "ethereum": "eth", "fantom": "ftm",
	"filecoin": "fil", "hedera": "hbar", "immutable": "imx", "immutable-x": "imx",
	"kusama": "ksm", "litecoin": "ltc", "oasis": "rose", "optimism": "op",
	"osmosis": "osmo", "polkadot": "dot", "polymesh": "polyx", "solana": "sol",
	"stacks": "stx", "stellar": "xlm", "zcash": "zec", "zetachain": "zeta",
}

const organizationWalletSyncExpectation = "Wallet data typically appears within 15 minutes but can take up to 24 hours, depending on transaction history volume and network load."

type orgWalletInput struct {
	Name                    string         `json:"name"`
	Description             string         `json:"description,omitempty"`
	Address                 string         `json:"address"`
	NetworkID               string         `json:"networkId"`
	SubsidiaryID            string         `json:"subsidiaryId,omitempty"`
	AddressType             string         `json:"addressType,omitempty"`
	SyncStartDateSEC        int64          `json:"syncStartDateSEC,omitempty"`
	ViewKey                 string         `json:"viewKey,omitempty"`
	Metadata                map[string]any `json:"metadata,omitempty"`
	IsBalanceMonitoringOnly bool           `json:"isBalanceMonitoringOnly,omitempty"`
}

type orgWalletAddFlags struct {
	transactionMutationFlags
	input                 string
	name                  string
	address               string
	network               string
	subsidiary            string
	addressType           string
	syncStartDateSEC      int64
	viewKey               string
	balanceMonitoringOnly bool
	allowDuplicate        bool
	concurrency           int
}

func newOrgWalletsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "wallets",
		Aliases: []string{"wallet"},
		Short:   "List or add wallets in the active Bitwave organization",
		Long: `Manage product wallets in the active Bitwave organization. This is
separate from the top-level local-ledger wallets command. Blockchain addresses
use the same accountBasedBlockchain creation contract as Bitwave Add Source.

After creation, data typically appears within 15 minutes but can take up to 24
hours depending on transaction history volume and network load.`,
	}
	walletExport := newTransactionExportCmd()
	walletExport.Use = "export"
	walletExport.Aliases = []string{"transactions-export", "txn-export"}
	walletExport.Short = "Export transactions for one or more wallets as CSV"
	walletExport.Long = `Export organization transactions using wallet, date, asset, and ignored-status filters.

This is the wallet-oriented route to the same validated export engine as
` + "`bitwave report transaction-export`" + `. Pass --ignored Ignored to include only
ignored transactions, or the API's supported ignored-status values as needed.`
	walletExport.Example = `  bitwave org wallets export --wallet "Treasury" --from 2026-01-01 --to 2026-06-30 --asset ETH --out treasury-eth.csv
  bitwave org wallets export --wallet wallet-id --all-dates --ignored Ignored --out ignored.csv`
	cmd.AddCommand(
		newOrgWalletsListCmd(), newOrgWalletsNetworksCmd(), newOrgWalletsAddCmd(),
		newOrgWalletStatusCmd("enable", false), newOrgWalletStatusCmd("disable", true),
		walletExport, newOrgWalletsRollupCmd(), newOrgWalletResyncCmd(),
	)
	return cmd
}

func newOrgWalletsListCmd() *cobra.Command {
	var orgID string
	var jsonOutput bool
	var disabledOnly, enabledOnly bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List organization wallets, including disabled and sync settings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if disabledOnly && enabledOnly {
				return errors.New("--disabled-only and --enabled-only cannot be combined")
			}
			resolvedOrg, err := resolveReportOrg(orgID)
			if err != nil {
				return err
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(resolvedOrg))
			wallets, err := client.DetailedOrgWallets(cmd.Context(), resolvedOrg)
			if err != nil {
				return fmt.Errorf("list organization wallets: %w", err)
			}
			filtered := wallets[:0]
			for _, wallet := range wallets {
				if disabledOnly && !wallet.Disabled {
					continue
				}
				if enabledOnly && wallet.Disabled {
					continue
				}
				filtered = append(filtered, wallet)
			}
			wallets = filtered
			if jsonOutput {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "organization": resolvedOrg, "wallets": wallets})
			}
			if len(wallets) == 0 {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "(no organization wallets)")
				return err
			}
			for _, wallet := range wallets {
				address := wallet.Address
				if address == "" && len(wallet.Addresses) > 0 {
					address = strings.Join(wallet.Addresses, ",")
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\tdisabled=%t\tcreatedSEC=%d\tsyncStartSEC=%d\n", wallet.ID, wallet.Name, wallet.NetworkID, address, wallet.SubsidiaryID, wallet.Disabled, wallet.CreatedSEC, wallet.Flags.SyncStartDateSEC)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().BoolVar(&disabledOnly, "disabled-only", false, "Return only disabled wallets")
	cmd.Flags().BoolVar(&enabledOnly, "enabled-only", false, "Return only enabled wallets")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func newOrgWalletStatusCmd(name string, disabled bool) *cobra.Command {
	var f transactionMutationFlags
	cmd := &cobra.Command{
		Use:   name + " WALLET_ID_OR_NAME [WALLET_ID_OR_NAME...]",
		Short: strings.ToUpper(name[:1]) + name[1:] + " one or more organization wallets",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			operation := name + "-organization-wallets"
			orgID, err := resolveReportOrg(f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(orgID))
			wallets, err := client.DetailedOrgWallets(cmd.Context(), orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("list wallets: %w", err))
			}
			selected := make([]orgreports.DetailedOrgWallet, 0, len(args))
			seen := map[string]bool{}
			for _, ref := range uniqueNonEmpty(args) {
				wallet, resolveErr := resolveDetailedOrgWallet(ref, wallets)
				if resolveErr != nil {
					return mutationError(cmd, operation, f.jsonOutput, resolveErr)
				}
				if !seen[wallet.ID] {
					seen[wallet.ID] = true
					selected = append(selected, *wallet)
				}
			}
			preview := map[string]any{"disabled": disabled, "wallets": selected}
			if f.dryRun {
				return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: operation, Organization: orgID, DryRun: true, Request: preview})
			}
			if !f.yes {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("refusing to change wallet status without --yes (use --dry-run to preview)"))
			}
			updated := make([]string, 0, len(selected))
			failed := make([]map[string]string, 0)
			for _, wallet := range selected {
				if wallet.Disabled == disabled {
					updated = append(updated, wallet.ID)
					continue
				}
				if updateErr := client.SetOrgWalletDisabled(cmd.Context(), orgID, wallet.ID, wallet.Name, disabled); updateErr != nil {
					failed = append(failed, map[string]string{"walletId": wallet.ID, "name": wallet.Name, "error": updateErr.Error()})
					continue
				}
				updated = append(updated, wallet.ID)
			}
			status := "success"
			if len(failed) > 0 {
				status = "partial_failure"
			}
			envelope := mutationEnvelope{SchemaVersion: "1", Status: status, Operation: operation, Organization: orgID, Result: map[string]any{"updated": updated, "failed": failed, "disabled": disabled}}
			if len(failed) > 0 {
				_ = writeJSON(cmd.OutOrStdout(), envelope)
				return fmt.Errorf("%s: %d updated, %d failed", operation, len(updated), len(failed))
			}
			return outputMutation(cmd, f.jsonOutput, envelope, fmt.Sprintf("%s: %d wallet(s)\n", operation, len(updated)))
		},
	}
	addMutationFlags(cmd, &f)
	return cmd
}

func resolveDetailedOrgWallet(ref string, wallets []orgreports.DetailedOrgWallet) (*orgreports.DetailedOrgWallet, error) {
	for index := range wallets {
		if wallets[index].ID == ref {
			return &wallets[index], nil
		}
	}
	matches := make([]int, 0)
	for index := range wallets {
		if strings.EqualFold(wallets[index].Name, ref) {
			matches = append(matches, index)
		}
	}
	if len(matches) == 1 {
		return &wallets[matches[0]], nil
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("wallet name %q is ambiguous; pass a wallet ID", ref)
	}
	return nil, fmt.Errorf("wallet %q not found; run `bitwave org wallets list --json`", ref)
}

func newOrgWalletsNetworksCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "networks",
		Short: "List canonical Bitwave blockchain network IDs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ids := make([]string, 0, len(organizationWalletNetworks))
			for id := range organizationWalletNetworks {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			if jsonOutput {
				items := make([]map[string]string, 0, len(ids))
				for _, id := range ids {
					items = append(items, map[string]string{"id": id, "name": organizationWalletNetworks[id]})
				}
				return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "networks": items, "forwardCompatible": true})
			}
			for _, id := range ids {
				fmt.Fprintf(cmd.OutOrStdout(), "%-10s %s\n", id, organizationWalletNetworks[id])
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func newOrgWalletsAddCmd() *cobra.Command {
	var f orgWalletAddFlags
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add one wallet, or a JSON batch, to the active organization",
		Long: `Add one organization wallet using flags, or supply --input with a JSON
array (or {"wallets": [...]}) for a batch. Address type defaults to address;
use --address-type hd for a BTC or DASH xpub/derivation key.

Creating a wallet starts asynchronous ingestion. Data typically appears within
15 minutes but can take up to 24 hours depending on transaction history volume
and network load.`,
		RunE: func(cmd *cobra.Command, _ []string) error { return runOrgWalletsAdd(cmd, f) },
	}
	addMutationFlags(cmd, &f.transactionMutationFlags)
	cmd.Flags().StringVarP(&f.input, "input", "i", "", "Wallet JSON file, or - for stdin")
	cmd.Flags().StringVar(&f.name, "name", "", "Wallet name (single-wallet mode)")
	cmd.Flags().StringVar(&f.address, "address", "", "Wallet address or HD derivation key")
	cmd.Flags().StringVar(&f.network, "network", "", "Canonical network ID or common network name")
	cmd.Flags().StringVar(&f.subsidiary, "subsidiary", "", "Subsidiary ID")
	cmd.Flags().StringVar(&f.addressType, "address-type", "address", "Address type: address or hd")
	cmd.Flags().Int64Var(&f.syncStartDateSEC, "sync-start-sec", 0, "Optional earliest sync time as Unix seconds")
	cmd.Flags().StringVar(&f.viewKey, "view-key", "", "Optional network view key (for example Aleo)")
	cmd.Flags().BoolVar(&f.balanceMonitoringOnly, "balance-monitoring-only", false, "Create as a balance-monitoring-only wallet")
	cmd.Flags().BoolVar(&f.allowDuplicate, "allow-duplicate", false, "Create even if the same network/address already exists")
	cmd.Flags().IntVar(&f.concurrency, "concurrency", 8, "Maximum concurrent wallet creations")
	return cmd
}

func runOrgWalletsAdd(cmd *cobra.Command, f orgWalletAddFlags) error {
	operation := "add-organization-wallets"
	inputs, err := loadOrgWalletInputs(f, cmd.InOrStdin())
	if err != nil {
		return mutationError(cmd, operation, f.jsonOutput, err)
	}
	for i := range inputs {
		if err := normalizeAndValidateOrgWallet(&inputs[i]); err != nil {
			return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("wallet %d: %w", i+1, err))
		}
	}
	if f.concurrency < 1 || f.concurrency > 50 {
		return mutationError(cmd, operation, f.jsonOutput, errors.New("--concurrency must be between 1 and 50"))
	}
	orgID, err := resolveReportOrg(f.orgID)
	if err != nil {
		return mutationError(cmd, operation, f.jsonOutput, err)
	}
	requests := make([]map[string]any, 0, len(inputs))
	for _, input := range inputs {
		requests = append(requests, buildOrgWalletPayload(input))
	}
	preview := map[string]any{"method": "POST", "url": "organization GraphQL createWallet", "wallets": requests}
	if f.dryRun {
		return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: operation, Organization: orgID, DryRun: true, Request: preview})
	}
	if !f.yes {
		return mutationError(cmd, operation, f.jsonOutput, errors.New("refusing to change the organization without --yes (use --dry-run to preview)"))
	}

	// Resolve the token once before fan-out. Refreshing and rewriting the same
	// credential file in every worker would serialize the fast path.
	token, err := makeOrgTokenResolver(orgID)()
	if err != nil {
		return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("resolve organization token: %w", err))
	}
	client := orgreports.New(resolveCoreBaseURL(), func() (string, error) { return token, nil })
	if err := validateWalletSubsidiaries(cmd, client, orgID, inputs); err != nil {
		return mutationError(cmd, operation, f.jsonOutput, err)
	}
	existing, err := client.Wallets(cmd.Context(), orgID)
	if err != nil {
		return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("check existing wallets: %w", err))
	}
	results := make([]map[string]any, len(inputs))
	jobs := make(chan int)
	workerCount := min(f.concurrency, len(inputs))
	var workers sync.WaitGroup
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				input := inputs[i]
				wallet, createErr := client.CreateOrgWallet(cmd.Context(), orgID, requests[i], nil)
				if createErr != nil {
					results[i] = map[string]any{"status": "failed", "input": input, "error": createErr.Error()}
					continue
				}
				results[i] = map[string]any{"status": "created", "input": input, "wallet": wallet}
			}
		}()
	}
	for i, input := range inputs {
		if !f.allowDuplicate {
			if wallet := findExistingOrgWallet(existing, input); wallet != nil {
				results[i] = map[string]any{"status": "skipped_existing", "input": input, "wallet": wallet}
				continue
			}
			for earlier := 0; earlier < i; earlier++ {
				if sameOrgWalletInput(inputs[earlier], input) {
					results[i] = map[string]any{"status": "skipped_duplicate_input", "input": input, "duplicateOf": earlier + 1}
					break
				}
			}
			if results[i] != nil {
				continue
			}
		}
		jobs <- i
	}
	close(jobs)
	workers.Wait()
	created, skipped, failed := 0, 0, 0
	for _, result := range results {
		if result["status"] == "created" {
			created++
		} else if result["status"] == "skipped_existing" || result["status"] == "skipped_duplicate_input" {
			skipped++
		} else if result["status"] == "failed" {
			failed++
		}
	}
	status := "success"
	if failed > 0 {
		status = "partial_failure"
	}
	syncGuidance := organizationWalletSyncGuidance()
	envelope := mutationEnvelope{SchemaVersion: "1", Status: status, Operation: operation, Organization: orgID, Result: map[string]any{"created": created, "skipped": skipped, "failed": failed, "concurrency": workerCount, "wallets": results, "syncGuidance": syncGuidance}}
	if failed > 0 {
		_ = writeJSON(cmd.OutOrStdout(), envelope)
		return fmt.Errorf("organization wallets: %d created, %d skipped, %d failed", created, skipped, failed)
	}
	human := fmt.Sprintf("organization wallets: %d created, %d skipped\n%s\nCheck progress: bitwave transaction search --wallet WALLET_NAME --limit 1 --json\n", created, skipped, organizationWalletSyncExpectation)
	return outputMutation(cmd, f.jsonOutput, envelope, human)
}

func organizationWalletSyncGuidance() map[string]any {
	return map[string]any{
		"expectedDuration": "15 minutes to 24 hours",
		"dependsOn":        []string{"transaction history volume", "network load"},
		"message":          organizationWalletSyncExpectation,
		"checkCommand":     "bitwave transaction search --wallet WALLET_NAME --limit 1 --json",
	}
}

func loadOrgWalletInputs(f orgWalletAddFlags, stdin io.Reader) ([]orgWalletInput, error) {
	if f.input == "" {
		return []orgWalletInput{{Name: f.name, Address: f.address, NetworkID: f.network, SubsidiaryID: f.subsidiary, AddressType: f.addressType, SyncStartDateSEC: f.syncStartDateSEC, ViewKey: f.viewKey, IsBalanceMonitoringOnly: f.balanceMonitoringOnly}}, nil
	}
	var data []byte
	var err error
	if f.input == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(f.input)
	}
	if err != nil {
		return nil, fmt.Errorf("read wallet input: %w", err)
	}
	var inputs []orgWalletInput
	if err = json.Unmarshal(data, &inputs); err == nil {
		return inputs, nil
	}
	var wrapped struct {
		Wallets []orgWalletInput `json:"wallets"`
	}
	if wrapErr := json.Unmarshal(data, &wrapped); wrapErr != nil {
		return nil, fmt.Errorf("decode wallet input JSON: %w", err)
	}
	return wrapped.Wallets, nil
}

func normalizeAndValidateOrgWallet(input *orgWalletInput) error {
	input.Name = strings.TrimSpace(input.Name)
	input.Address = strings.TrimSpace(input.Address)
	input.NetworkID = strings.ToLower(strings.TrimSpace(input.NetworkID))
	input.SubsidiaryID = strings.TrimSpace(input.SubsidiaryID)
	input.AddressType = strings.ToLower(strings.TrimSpace(input.AddressType))
	if alias := organizationWalletNetworkAliases[input.NetworkID]; alias != "" {
		input.NetworkID = alias
	}
	if input.Name == "" {
		return errors.New("name is required")
	}
	if input.Address == "" {
		return errors.New("address is required")
	}
	if input.NetworkID == "" {
		return errors.New("networkId is required")
	}
	if input.AddressType == "" {
		input.AddressType = "address"
	}
	if input.AddressType != "address" && input.AddressType != "hd" {
		return errors.New("addressType must be address or hd")
	}
	if input.AddressType == "hd" && input.NetworkID != "btc" && input.NetworkID != "dash" {
		return fmt.Errorf("HD wallets are supported only for btc and dash, not %s", input.NetworkID)
	}
	if input.SyncStartDateSEC < 0 {
		return errors.New("syncStartDateSEC cannot be negative")
	}
	if strings.TrimSpace(input.Description) != "" {
		return errors.New("the Bitwave WalletInput contract does not accept description during creation")
	}
	return nil
}

func buildOrgWalletPayload(input orgWalletInput) map[string]any {
	var wallet map[string]any
	if input.AddressType == "hd" {
		wallet = map[string]any{"name": input.Name, "type": "watch", "watch": map[string]any{"coin": strings.ToUpper(input.NetworkID), "type": "hd", "derivationKey": input.Address}}
	} else {
		blockchain := map[string]any{"address": input.Address, "networkId": input.NetworkID}
		if input.ViewKey != "" {
			blockchain["viewKey"] = input.ViewKey
		}
		if len(input.Metadata) > 0 {
			blockchain["metadata"] = input.Metadata
		}
		wallet = map[string]any{"name": input.Name, "type": "accountBasedBlockchain", "accountBasedBlockchain": blockchain}
		if input.NetworkID == "canton" {
			wallet["structuredSyncerVersionConfig"] = map[string]any{"canton-sync-svc": map[string]any{"canton-incremental-sync-workflow": []map[string]int{{"version": 3}}, "canton-full-sync-workflow": []map[string]int{{"version": 3}}}}
		}
	}
	if input.SubsidiaryID != "" {
		wallet["subsidiaryId"] = input.SubsidiaryID
	}
	if input.SyncStartDateSEC > 0 {
		wallet["flags"] = map[string]any{"syncStartDateSEC": input.SyncStartDateSEC}
	}
	if input.IsBalanceMonitoringOnly {
		wallet["isBalanceMonitoringOnly"] = true
	}
	return wallet
}

func validateWalletSubsidiaries(cmd *cobra.Command, client *orgreports.Client, orgID string, inputs []orgWalletInput) error {
	wanted := map[string]bool{}
	for _, input := range inputs {
		if input.SubsidiaryID != "" {
			wanted[input.SubsidiaryID] = true
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	subsidiaries, err := client.Subsidiaries(cmd.Context(), orgID)
	if err != nil {
		return fmt.Errorf("validate subsidiaries: %w", err)
	}
	for _, subsidiary := range subsidiaries {
		delete(wanted, subsidiary.ID)
	}
	if len(wanted) > 0 {
		missing := make([]string, 0, len(wanted))
		for id := range wanted {
			missing = append(missing, id)
		}
		sort.Strings(missing)
		return fmt.Errorf("unknown subsidiary ID(s): %s", strings.Join(missing, ", "))
	}
	return nil
}

func findExistingOrgWallet(wallets []orgreports.Wallet, input orgWalletInput) *orgreports.Wallet {
	for i := range wallets {
		wallet := &wallets[i]
		if !strings.EqualFold(wallet.NetworkID, input.NetworkID) {
			continue
		}
		addresses := append([]string{wallet.Address}, wallet.Addresses...)
		for _, address := range addresses {
			if strings.EqualFold(strings.TrimSpace(address), input.Address) {
				return wallet
			}
		}
	}
	return nil
}

func sameOrgWalletInput(a, b orgWalletInput) bool {
	return strings.EqualFold(a.NetworkID, b.NetworkID) && strings.EqualFold(a.Address, b.Address)
}
