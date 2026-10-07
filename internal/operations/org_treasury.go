package operations

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"

	op "github.com/bitwave-io/bitwave-cli/internal/operation"
	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

// Treasury monitoring is served by treasury-svc and analytics-query-svc
// behind the core API gateway (https://api.bitwave.io/v3/orgs/{org}/...):
//
//	/v3/orgs/{org}/monitoring-configs[/{id}[/balance-history]]
//	/v3/orgs/{org}/alerts[/{id}/acknowledge]
//	/v3/orgs/{org}/alert-channels
//	/v3/orgs/{org}/analytics/templates[/{id}]
//	/v3/orgs/{org}/analytics/queries/execute
//
// Client-side validation mirrors treasury-svc domain.ValidateThresholds and
// domain.AlertChannelSetting.Validate, and additionally requires explicit
// threshold values (the backend silently treats a missing value as 0).

const (
	treasuryModeStatic  = "STATIC"
	treasuryModeDynamic = "DYNAMIC"
	treasuryMaxHistory  = 90
)

var (
	treasuryTiers          = []string{"APPROACHING", "BREACHING"}
	treasuryAlertStatuses  = []string{"ACTIVE", "ACKNOWLEDGED", "RESOLVED", "OPEN"}
	treasuryChannels       = []string{"EMAIL", "SLACK", "WEBHOOK"}
	treasuryCostTiers      = []string{"small", "medium", "large"}
	treasuryEmailPattern   = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	treasuryDefaultNotify  = []string{"APPROACHING", "BREACHING"}
	treasuryTemplateTagDef = "treasury"
)

// treasuryRule is the request shape of domain.DynamicThresholdRule. Server-
// populated evaluation fields (lastResult, lastEvaluatedAt, lastError) are
// intentionally absent so they are never written back.
type treasuryRule struct {
	TemplateID   string         `json:"templateId"`
	Parameters   map[string]any `json:"parameters"`
	Predicate    string         `json:"predicate"`
	ResultColumn string         `json:"resultColumn,omitempty"`
}

// treasuryThreshold is domain.ThresholdConfig. Value is a pointer only so the
// CLI can tell an omitted value from an explicit 0; it is always set before a
// request is sent.
type treasuryThreshold struct {
	Mode  string        `json:"mode"`
	Value *float64      `json:"value"`
	Rule  *treasuryRule `json:"rule,omitempty"`
}

type treasuryCreateBody struct {
	WalletID     string            `json:"walletId"`
	WalletName   string            `json:"walletName"`
	Network      string            `json:"network"`
	Address      string            `json:"address"`
	CurrencyID   int64             `json:"currencyId"`
	CurrencyType string            `json:"currencyType"`
	AlertEnabled bool              `json:"alertEnabled"`
	NotifyOn     []string          `json:"notifyOn,omitempty"`
	Approaching  treasuryThreshold `json:"approaching"`
	Breaching    treasuryThreshold `json:"breaching"`
}

type treasuryUpdateBody struct {
	Approaching  *treasuryThreshold `json:"approaching,omitempty"`
	Breaching    *treasuryThreshold `json:"breaching,omitempty"`
	AlertEnabled *bool              `json:"alertEnabled,omitempty"`
	NotifyOn     []string           `json:"notifyOn,omitempty"`
}

type treasuryChannel struct {
	Channel string `json:"channel"`
	Enabled bool   `json:"enabled"`
	Target  string `json:"target"`
}

type treasuryChannelsBody struct {
	Channels []treasuryChannel `json:"channels"`
}

// treasuryMonitor is the subset of domain.MonitoringConfig the CLI reads.
type treasuryMonitor struct {
	ID           string            `json:"id"`
	WalletID     string            `json:"walletId"`
	WalletName   string            `json:"walletName"`
	CurrencyID   int64             `json:"currencyId"`
	CurrencyType string            `json:"currencyType"`
	Approaching  treasuryThreshold `json:"approaching"`
	Breaching    treasuryThreshold `json:"breaching"`
	AlertEnabled bool              `json:"alertEnabled"`
	LastStatus   string            `json:"lastStatus"`
	LastBalance  *float64          `json:"lastBalance"`
}

func (m treasuryMonitor) dynamic() bool {
	return strings.EqualFold(m.Approaching.Mode, treasuryModeDynamic) || strings.EqualFold(m.Breaching.Mode, treasuryModeDynamic)
}

func newTreasuryCmd() *op.Definition {
	cmd := &op.Definition{
		Use:   "treasury",
		Short: "Monitor wallet balance thresholds, alerts, and alert channels",
		Long: `Manage Bitwave Treasury monitoring for the active organization.

A monitor watches one wallet currency against an approaching and a breaching
threshold. Each tier is STATIC (a fixed balance floor) or DYNAMIC (a predicate
evaluated against an analytics query template, e.g. '$balance < $earned * 0.7').
Monitors fire alerts; alerts are delivered through the organization's alert
channels.

Every write supports --dry-run (prints the exact request) and requires --yes.`,
	}
	monitors := &op.Definition{Use: "monitors", Aliases: []string{"monitor"}, Short: "List, create, update, and delete wallet threshold monitors"}
	monitors.AddCommand(newTreasuryMonitorsListCmd(), newTreasuryMonitorGetCmd(), newTreasuryMonitorCreateCmd(), newTreasuryMonitorUpdateCmd(),
		newTreasuryMonitorDeleteCmd(), newTreasuryMonitorHistoryCmd(), newTreasuryMonitorReevaluateCmd())
	alerts := &op.Definition{Use: "alerts", Aliases: []string{"alert"}, Short: "List and acknowledge treasury alerts"}
	alerts.AddCommand(newTreasuryAlertsListCmd(), newTreasuryAlertAckCmd())
	channels := &op.Definition{Use: "channels", Aliases: []string{"channel"}, Short: "Read and set alert delivery channels"}
	channels.AddCommand(newTreasuryChannelsGetCmd(), newTreasuryChannelsSetCmd())
	templates := &op.Definition{Use: "templates", Aliases: []string{"template"}, Short: "Discover and run analytics query templates used by dynamic thresholds"}
	templates.AddCommand(newTreasuryTemplatesListCmd(), newTreasuryTemplateGetCmd(), newTreasuryTemplateExecuteCmd())
	cmd.AddCommand(monitors, alerts, channels, templates)
	return cmd
}

// treasuryPath builds /v3/orgs/{org}/<segments...>, escaping every segment.
func treasuryPath(orgID string, segments ...string) string {
	var b strings.Builder
	b.WriteString("/v3/orgs/")
	b.WriteString(url.PathEscape(orgID))
	for _, segment := range segments {
		b.WriteByte('/')
		b.WriteString(url.PathEscape(segment))
	}
	return b.String()
}

func treasuryQuery(path string, values url.Values) string {
	if len(values) == 0 {
		return path
	}
	return path + "?" + values.Encode()
}

// ---- Reads -----------------------------------------------------------------

type treasuryReadFlags struct {
	orgID      string
	jsonOutput bool
}

func addTreasuryReadFlags(cmd *op.Definition, f *treasuryReadFlags, jsonDefault bool) {
	cmd.Flags().StringVar(&f.orgID, "org", "", "Organization ID override")
	usage := "Emit machine-readable JSON instead of a table"
	if jsonDefault {
		usage = "Emit machine-readable JSON (the only supported format)"
	}
	cmd.Flags().BoolVar(&f.jsonOutput, "json", jsonDefault, usage)
}

func treasuryGet(cmd *op.Call, explicitOrg, path string) (string, []byte, error) {
	orgID, err := resolveReportOrg(cmd.Context(), explicitOrg)
	if err != nil {
		return "", nil, err
	}
	data, err := newReportsClient(cmd.Context(), orgID).RawRequest(cmd.Context(), orgreports.APIServiceCore, http.MethodGet, path, nil)
	return orgID, data, err
}

func treasuryGetPath(cmd *op.Call, explicitOrg string, build func(orgID string) string) (string, []byte, error) {
	orgID, err := resolveReportOrg(cmd.Context(), explicitOrg)
	if err != nil {
		return "", nil, err
	}
	return treasuryGet(cmd, orgID, build(orgID))
}

func decodeArray(data []byte, what string) ([]json.RawMessage, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("unexpected %s response (expected a JSON array): %w", what, err)
	}
	if items == nil {
		items = []json.RawMessage{}
	}
	return items, nil
}

func writeRaw(cmd *op.Call, data []byte) error {
	var pretty bytes.Buffer
	if json.Indent(&pretty, data, "", "  ") == nil {
		data = pretty.Bytes()
	}
	_, err := cmd.OutOrStdout().Write(append(bytes.TrimRight(data, "\n"), '\n'))
	return err
}

func newTreasuryMonitorsListCmd() *op.Definition {
	var f treasuryReadFlags
	cmd := &op.Definition{
		Use:   "list",
		Short: "List wallet threshold monitors",
		Args:  op.NoArgs,
		RunE: func(cmd *op.Call, _ []string) error {
			orgID, data, err := treasuryGetPath(cmd, f.orgID, func(org string) string { return treasuryPath(org, "monitoring-configs") })
			if err != nil {
				return fmt.Errorf("list treasury monitors: %w", err)
			}
			items, err := decodeArray(data, "monitoring-configs")
			if err != nil {
				return err
			}
			if f.jsonOutput {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "organization": orgID, "count": len(items), "monitors": items})
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tWALLET\tCURRENCY\tAPPROACHING\tBREACHING\tSTATUS\tBALANCE\tALERTS")
			for _, raw := range items {
				var m treasuryMonitor
				if json.Unmarshal(raw, &m) != nil {
					continue
				}
				balance := "-"
				if m.LastBalance != nil {
					balance = formatTreasuryNumber(*m.LastBalance)
				}
				status := m.LastStatus
				if status == "" {
					status = "PENDING"
				}
				fmt.Fprintf(w, "%s\t%s\t%s.%d\t%s\t%s\t%s\t%s\t%t\n", m.ID, firstNonEmpty(m.WalletName, m.WalletID), strings.ToUpper(m.CurrencyType), m.CurrencyID,
					describeTreasuryTier(m.Approaching), describeTreasuryTier(m.Breaching), status, balance, m.AlertEnabled)
			}
			return w.Flush()
		},
	}
	addTreasuryReadFlags(cmd, &f, false)
	return cmd
}

func newTreasuryMonitorGetCmd() *op.Definition {
	var f treasuryReadFlags
	cmd := &op.Definition{
		Use:   "get MONITOR_ID",
		Short: "Get one wallet threshold monitor",
		Args:  op.ExactArgs(1),
		RunE: func(cmd *op.Call, args []string) error {
			_, data, err := treasuryGetPath(cmd, f.orgID, func(org string) string { return treasuryPath(org, "monitoring-configs", args[0]) })
			if err != nil {
				return fmt.Errorf("get treasury monitor %s: %w", args[0], err)
			}
			return writeRaw(cmd, data)
		},
	}
	addTreasuryReadFlags(cmd, &f, true)
	return cmd
}

func newTreasuryMonitorHistoryCmd() *op.Definition {
	var f treasuryReadFlags
	var days int
	cmd := &op.Definition{
		Use:   "history MONITOR_ID",
		Short: "Show a monitor's daily available balance history",
		Long: `Show the monitored currency's daily available balance for the monitor's wallet.

Only days with an observed balance are returned (UTC, ascending). --days
defaults to 14 and may be at most 90.`,
		Args: op.ExactArgs(1),
		RunE: func(cmd *op.Call, args []string) error {
			if days < 1 || days > treasuryMaxHistory {
				return fmt.Errorf("--days must be between 1 and %d", treasuryMaxHistory)
			}
			orgID, data, err := treasuryGetPath(cmd, f.orgID, func(org string) string {
				return treasuryQuery(treasuryPath(org, "monitoring-configs", args[0], "balance-history"), url.Values{"days": {fmt.Sprint(days)}})
			})
			if err != nil {
				return fmt.Errorf("get balance history for monitor %s: %w", args[0], err)
			}
			var history struct {
				CurrencyID   int64  `json:"currencyId"`
				CurrencyType string `json:"currencyType"`
				Points       []struct {
					Day     string  `json:"day"`
					Balance float64 `json:"balance"`
				} `json:"points"`
			}
			if err := json.Unmarshal(data, &history); err != nil {
				return fmt.Errorf("unexpected balance-history response: %w", err)
			}
			if f.jsonOutput {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "organization": orgID, "monitorId": args[0], "days": days, "history": json.RawMessage(data)})
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "DAY\tBALANCE")
			for _, point := range history.Points {
				fmt.Fprintf(w, "%s\t%s\n", point.Day, formatTreasuryNumber(point.Balance))
			}
			return w.Flush()
		},
	}
	addTreasuryReadFlags(cmd, &f, false)
	cmd.Flags().IntVar(&days, "days", 14, "Number of days of history (1-90)")
	return cmd
}

func newTreasuryAlertsListCmd() *op.Definition {
	var f treasuryReadFlags
	var status, tier string
	cmd := &op.Definition{
		Use:   "list",
		Short: "List treasury alerts, newest first",
		Long: `List the organization's treasury alerts, newest first.

--status filters by ACTIVE, ACKNOWLEDGED, RESOLVED, or OPEN (ACTIVE or
ACKNOWLEDGED). --tier filters by APPROACHING or BREACHING. Filters are applied
by treasury-svc.`,
		Args: op.NoArgs,
		RunE: func(cmd *op.Call, _ []string) error {
			query := url.Values{}
			if status != "" {
				normalized, err := oneOf("--status", status, treasuryAlertStatuses)
				if err != nil {
					return err
				}
				query.Set("status", normalized)
			}
			if tier != "" {
				normalized, err := oneOf("--tier", tier, treasuryTiers)
				if err != nil {
					return err
				}
				query.Set("tier", normalized)
			}
			orgID, data, err := treasuryGetPath(cmd, f.orgID, func(org string) string { return treasuryQuery(treasuryPath(org, "alerts"), query) })
			if err != nil {
				return fmt.Errorf("list treasury alerts: %w", err)
			}
			items, err := decodeArray(data, "alerts")
			if err != nil {
				return err
			}
			if f.jsonOutput {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "organization": orgID, "count": len(items), "filters": map[string]string{"status": query.Get("status"), "tier": query.Get("tier")}, "alerts": items})
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tTIER\tSTATUS\tWALLET\tBALANCE\tTHRESHOLD\tFIRED")
			for _, raw := range items {
				var a struct {
					ID         string  `json:"id"`
					Tier       string  `json:"tier"`
					Status     string  `json:"status"`
					WalletID   string  `json:"walletId"`
					WalletName string  `json:"walletName"`
					Balance    float64 `json:"balance"`
					Threshold  float64 `json:"threshold"`
					FiredAt    string  `json:"firedAt"`
				}
				if json.Unmarshal(raw, &a) != nil {
					continue
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", a.ID, a.Tier, a.Status, firstNonEmpty(a.WalletName, a.WalletID), formatTreasuryNumber(a.Balance), formatTreasuryNumber(a.Threshold), a.FiredAt)
			}
			return w.Flush()
		},
	}
	addTreasuryReadFlags(cmd, &f, false)
	cmd.Flags().StringVar(&status, "status", "", "Filter by status: ACTIVE, ACKNOWLEDGED, RESOLVED, or OPEN")
	cmd.Flags().StringVar(&tier, "tier", "", "Filter by tier: APPROACHING or BREACHING")
	return cmd
}

func newTreasuryChannelsGetCmd() *op.Definition {
	var f treasuryReadFlags
	cmd := &op.Definition{
		Use:   "get",
		Short: "Show alert delivery channel settings",
		Long: `Show the organization's treasury alert delivery channels.

Only EMAIL is delivered today; SLACK and WEBHOOK settings are stored ahead of
delivery support. An organization that never saved settings returns no rows.`,
		Args: op.NoArgs,
		RunE: func(cmd *op.Call, _ []string) error {
			orgID, data, err := treasuryGetPath(cmd, f.orgID, func(org string) string { return treasuryPath(org, "alert-channels") })
			if err != nil {
				return fmt.Errorf("get treasury alert channels: %w", err)
			}
			var body struct {
				Channels *[]json.RawMessage `json:"channels"`
			}
			if err := json.Unmarshal(data, &body); err != nil || body.Channels == nil {
				return fmt.Errorf("unexpected alert-channels response (expected {\"channels\":[...]}): %s", strings.TrimSpace(string(data)))
			}
			channels := *body.Channels
			if channels == nil {
				channels = []json.RawMessage{}
			}
			if f.jsonOutput {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "organization": orgID, "channels": channels})
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "CHANNEL\tENABLED\tTARGET\tUPDATED")
			for _, raw := range channels {
				var c struct {
					treasuryChannel
					UpdatedAt string `json:"updatedAt"`
				}
				if json.Unmarshal(raw, &c) == nil {
					fmt.Fprintf(w, "%s\t%t\t%s\t%s\n", c.Channel, c.Enabled, c.Target, c.UpdatedAt)
				}
			}
			return w.Flush()
		},
	}
	addTreasuryReadFlags(cmd, &f, false)
	return cmd
}

func newTreasuryTemplatesListCmd() *op.Definition {
	var f treasuryReadFlags
	var tags []string
	var all bool
	cmd := &op.Definition{
		Use:   "list",
		Short: "List analytics query templates available to dynamic thresholds",
		Long: `List analytics-query-svc templates for the "Dynamic (query)" threshold picker.

By default only templates tagged "treasury" are returned, matching the
Treasury UI. Use --tag to choose other tags (repeatable) or --all for every
template. Output columns of a template are the $variables a predicate may use.`,
		Args: op.NoArgs,
		RunE: func(cmd *op.Call, _ []string) error {
			if all && len(tags) > 0 {
				return errors.New("use either --tag or --all")
			}
			query := url.Values{}
			if !all {
				if len(tags) == 0 {
					tags = []string{treasuryTemplateTagDef}
				}
				query.Set("tags", strings.Join(tags, ","))
			}
			orgID, data, err := treasuryGetPath(cmd, f.orgID, func(org string) string { return treasuryQuery(treasuryPath(org, "analytics", "templates"), query) })
			if err != nil {
				return fmt.Errorf("list analytics templates: %w", err)
			}
			items, err := decodeArray(data, "analytics templates")
			if err != nil {
				return err
			}
			if f.jsonOutput {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "organization": orgID, "count": len(items), "tags": tags, "templates": items})
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tCOST\tPARAMETERS\tOUTPUT COLUMNS")
			for _, raw := range items {
				var t struct {
					ID         string `json:"id"`
					Name       string `json:"name"`
					CostTier   string `json:"costTier"`
					Parameters []struct {
						Name     string `json:"name"`
						Required bool   `json:"required"`
					} `json:"parameters"`
					OutputColumns []struct {
						Name string `json:"name"`
					} `json:"outputColumns"`
				}
				if json.Unmarshal(raw, &t) != nil {
					continue
				}
				params := make([]string, 0, len(t.Parameters))
				for _, p := range t.Parameters {
					name := p.Name
					if p.Required {
						name += "*"
					}
					params = append(params, name)
				}
				columns := make([]string, 0, len(t.OutputColumns))
				for _, c := range t.OutputColumns {
					columns = append(columns, "$"+c.Name)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", t.ID, t.Name, t.CostTier, strings.Join(params, ","), strings.Join(columns, ","))
			}
			return w.Flush()
		},
	}
	addTreasuryReadFlags(cmd, &f, false)
	cmd.Flags().StringArrayVar(&tags, "tag", nil, "Template tag filter (repeatable; default treasury)")
	cmd.Flags().BoolVar(&all, "all", false, "List templates with any tag")
	return cmd
}

func newTreasuryTemplateGetCmd() *op.Definition {
	var f treasuryReadFlags
	cmd := &op.Definition{
		Use:   "get TEMPLATE_ID",
		Short: "Get one analytics query template, including parameters and output columns",
		Args:  op.ExactArgs(1),
		RunE: func(cmd *op.Call, args []string) error {
			_, data, err := treasuryGetPath(cmd, f.orgID, func(org string) string { return treasuryPath(org, "analytics", "templates", args[0]) })
			if err != nil {
				return fmt.Errorf("get analytics template %s: %w", args[0], err)
			}
			return writeRaw(cmd, data)
		},
	}
	addTreasuryReadFlags(cmd, &f, true)
	return cmd
}

// ---- Mutations -------------------------------------------------------------

type treasuryRequest struct {
	method string
	path   string
	body   any
}

func (r treasuryRequest) preview(client *orgreports.Client) (map[string]any, error) {
	endpoint, err := client.RawEndpoint(orgreports.APIServiceCore, r.path)
	if err != nil {
		return nil, err
	}
	preview := map[string]any{"method": r.method, "url": endpoint}
	if r.body != nil {
		preview["body"] = r.body
	}
	return preview, nil
}

func (r treasuryRequest) send(cmd *op.Call, client *orgreports.Client) (json.RawMessage, error) {
	data, err := client.RawRequest(cmd.Context(), orgreports.APIServiceCore, r.method, r.path, r.body)
	if err != nil {
		return nil, err
	}
	if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 && json.Valid(trimmed) {
		return json.RawMessage(trimmed), nil
	}
	return nil, nil
}

// runTreasuryMutation applies the CLI mutation contract to one request:
// --dry-run prints the exact request and sends nothing; otherwise --yes is
// required. human renders the non-JSON success line from the response.
func runTreasuryMutation(cmd *op.Call, f transactionMutationFlags, orgID, operation, refusal string, request treasuryRequest, human func(json.RawMessage) string) error {
	client := newReportsClient(cmd.Context(), orgID)
	preview, err := request.preview(client)
	if err != nil {
		return mutationError(cmd, operation, f.jsonOutput, err)
	}
	if f.dryRun {
		return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: operation, Organization: orgID, DryRun: true, Request: preview})
	}
	if !f.yes {
		return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("refusing to %s without --yes (use --dry-run to preview)", refusal))
	}
	result, err := request.send(cmd, client)
	if err != nil {
		return mutationError(cmd, operation, f.jsonOutput, err)
	}
	var output any
	if result != nil {
		output = result
	}
	return outputMutation(cmd, f.jsonOutput, mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: operation, Organization: orgID, Request: preview, Result: output}, human(result))
}

func resultID(result json.RawMessage) string {
	var object struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(result, &object)
	return object.ID
}

// treasuryTierFlags are the per-tier (approaching/breaching) threshold flags.
type treasuryTierFlags struct {
	name      string
	value     float64
	template  string
	predicate string
	params    []string
}

func addTreasuryTierFlags(cmd *op.Definition, t *treasuryTierFlags, name string) {
	t.name = name
	cmd.Flags().Float64Var(&t.value, name, 0, "STATIC "+name+" threshold (fires when balance <= value); the seed/display value for a DYNAMIC tier")
	cmd.Flags().StringVar(&t.template, name+"-template", "", "Analytics query template ID for a DYNAMIC "+name+" tier")
	cmd.Flags().StringVar(&t.predicate, name+"-predicate", "", "Predicate for a DYNAMIC "+name+" tier, e.g. '$balance < $total_earned * 0.702'")
	cmd.Flags().StringArrayVar(&t.params, name+"-param", nil, "Template parameter key=value for the "+name+" tier (repeatable; JSON literals such as 10, true, or \"10\" keep their type)")
}

func (t *treasuryTierFlags) flagNames() []string {
	return []string{t.name, t.name + "-template", t.name + "-predicate", t.name + "-param"}
}

// build returns the tier, or nil when none of its flags were given.
func (t *treasuryTierFlags) build(cmd *op.Call) (*treasuryThreshold, error) {
	changed := func(suffix string) bool { return cmd.Flags().Changed(t.name + suffix) }
	dynamic := changed("-template") || changed("-predicate") || changed("-param")
	if !dynamic && !changed("") {
		return nil, nil
	}
	value := t.value
	if !dynamic {
		return &treasuryThreshold{Mode: treasuryModeStatic, Value: &value}, nil
	}
	if strings.TrimSpace(t.template) == "" && len(t.params) > 0 {
		return nil, fmt.Errorf("--%s-param requires --%s-template", t.name, t.name)
	}
	params, err := parseTreasuryParams("--"+t.name+"-param", t.params)
	if err != nil {
		return nil, err
	}
	return &treasuryThreshold{Mode: treasuryModeDynamic, Value: &value, Rule: &treasuryRule{
		TemplateID: strings.TrimSpace(t.template), Parameters: params, Predicate: strings.TrimSpace(t.predicate),
	}}, nil
}

// parseTreasuryParams parses key=value pairs. A value that is a JSON literal
// keeps its JSON type (BigQuery infers parameter types from it); anything else
// is sent as a string.
func parseTreasuryParams(flag string, pairs []string) (map[string]any, error) {
	params := map[string]any{}
	for _, pair := range pairs {
		key, raw, ok := strings.Cut(pair, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("%s must be key=value, got %q", flag, pair)
		}
		if _, dup := params[key]; dup {
			return nil, fmt.Errorf("%s: duplicate parameter %q", flag, key)
		}
		params[key] = treasuryParamValue(raw)
	}
	return params, nil
}

func treasuryParamValue(raw string) any {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return raw
	}
	if _, err := decoder.Token(); err != io.EOF {
		return raw
	}
	return value
}

// normalizeTreasuryTier mirrors treasury-svc validateTier and requires an
// explicit value for STATIC tiers.
func normalizeTreasuryTier(name string, t *treasuryThreshold) error {
	t.Mode = strings.ToUpper(strings.TrimSpace(t.Mode))
	if t.Mode == "" {
		t.Mode = treasuryModeStatic
	}
	switch t.Mode {
	case treasuryModeStatic:
		if t.Value == nil {
			return fmt.Errorf("%s threshold value is required (the backend would treat a missing value as 0)", name)
		}
		if math.IsNaN(*t.Value) || math.IsInf(*t.Value, 0) {
			return fmt.Errorf("%s threshold must be a number", name)
		}
		if *t.Value < 0 {
			return fmt.Errorf("%s threshold must be zero or greater", name)
		}
		if t.Rule != nil {
			return fmt.Errorf("%s threshold is STATIC but has a rule; set mode DYNAMIC or remove the rule", name)
		}
	case treasuryModeDynamic:
		if t.Rule == nil {
			return fmt.Errorf("%s threshold is dynamic but has no rule", name)
		}
		t.Rule.Predicate = strings.TrimSpace(t.Rule.Predicate)
		if t.Rule.Predicate == "" && strings.TrimSpace(t.Rule.ResultColumn) == "" {
			return fmt.Errorf("%s threshold rule needs a predicate (--%s-predicate)", name, name)
		}
		if t.Rule.Parameters == nil {
			t.Rule.Parameters = map[string]any{}
		}
		if t.Value == nil {
			zero := 0.0
			t.Value = &zero
		}
		if math.IsNaN(*t.Value) || math.IsInf(*t.Value, 0) {
			return fmt.Errorf("%s threshold must be a number", name)
		}
	default:
		return fmt.Errorf("%s threshold mode must be %q or %q", name, treasuryModeStatic, treasuryModeDynamic)
	}
	return nil
}

// validateTreasuryThresholds validates whichever tiers are present. The
// ordering rule applies only when both are known and both are STATIC.
func validateTreasuryThresholds(approaching, breaching *treasuryThreshold) error {
	if approaching != nil {
		if err := normalizeTreasuryTier("approaching", approaching); err != nil {
			return err
		}
	}
	if breaching != nil {
		if err := normalizeTreasuryTier("breaching", breaching); err != nil {
			return err
		}
	}
	if approaching != nil && breaching != nil && approaching.Mode == treasuryModeStatic && breaching.Mode == treasuryModeStatic && *breaching.Value >= *approaching.Value {
		return errors.New("breaching threshold must be strictly less than approaching threshold")
	}
	return nil
}

func normalizeNotifyOn(values []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if strings.TrimSpace(part) == "" {
				continue
			}
			tier, err := oneOf("notifyOn", part, treasuryTiers)
			if err != nil {
				return nil, err
			}
			if !seen[tier] {
				seen[tier] = true
				out = append(out, tier)
			}
		}
	}
	if len(values) > 0 && len(out) == 0 {
		return nil, errors.New("notifyOn needs at least one of APPROACHING or BREACHING")
	}
	return out, nil
}

func normalizeCurrencyType(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "coin":
		return "Coin", nil
	case "fiat":
		return "Fiat", nil
	}
	return "", fmt.Errorf("currencyType must be Coin or Fiat, got %q", value)
}

func oneOf(flag, value string, allowed []string) (string, error) {
	for _, candidate := range allowed {
		if strings.EqualFold(strings.TrimSpace(value), candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%s must be one of %s, got %q", flag, strings.Join(allowed, ", "), value)
}

// readTreasuryInput strictly decodes a JSON object from a file or stdin.
func readTreasuryInput(cmd *op.Call, path string, target any) error {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(io.LimitReader(cmd.InOrStdin(), 4<<20))
	} else {
		data, err = op.RuntimeFrom(cmd.Context()).Files.ReadFile(path)
	}
	if err != nil {
		return fmt.Errorf("read --input: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("--input must be a JSON object matching the request contract: %w", err)
	}
	if decoder.More() {
		return errors.New("--input must contain exactly one JSON object")
	}
	return nil
}

func changedAny(cmd *op.Call, names ...string) []string {
	var changed []string
	for _, name := range names {
		if cmd.Flags().Changed(name) {
			changed = append(changed, "--"+name)
		}
	}
	return changed
}

type treasuryMonitorFlags struct {
	transactionMutationFlags
	input        string
	walletID     string
	walletName   string
	network      string
	address      string
	currencyID   int64
	currencyType string
	alertEnabled bool
	notifyOn     []string
	approaching  treasuryTierFlags
	breaching    treasuryTierFlags
}

func (f *treasuryMonitorFlags) bodyFlagNames() []string {
	names := []string{"wallet", "wallet-name", "network", "address", "currency-id", "currency-type", "alert-enabled", "notify-on"}
	names = append(names, f.approaching.flagNames()...)
	return append(names, f.breaching.flagNames()...)
}

func addTreasuryMonitorBodyFlags(cmd *op.Definition, f *treasuryMonitorFlags, create bool) {
	addMutationFlags(cmd, &f.transactionMutationFlags)
	cmd.Flags().StringVarP(&f.input, "input", "i", "", "Complete request body JSON file, or - for stdin (instead of body flags)")
	if create {
		cmd.Flags().StringVar(&f.walletID, "wallet", "", "Bitwave wallet ID to monitor (required)")
		cmd.Flags().StringVar(&f.walletName, "wallet-name", "", "Wallet display name shown on alerts")
		cmd.Flags().StringVar(&f.network, "network", "", "Wallet network ID (e.g. eth, canton)")
		cmd.Flags().StringVar(&f.address, "address", "", "Wallet address")
		cmd.Flags().Int64Var(&f.currencyID, "currency-id", 0, "Monitored currency ID (Bitwave coin or fiat ID, e.g. 10 for ETH) (required)")
		cmd.Flags().StringVar(&f.currencyType, "currency-type", "Coin", "Monitored currency type: Coin or Fiat")
		cmd.Flags().BoolVar(&f.alertEnabled, "alert-enabled", true, "Send notifications when the monitor changes tier")
		cmd.Flags().StringSliceVar(&f.notifyOn, "notify-on", nil, "Tiers that notify: APPROACHING, BREACHING (default both, like the Treasury UI)")
	} else {
		cmd.Flags().BoolVar(&f.alertEnabled, "alert-enabled", false, "Enable or disable notifications (only sent when given)")
		cmd.Flags().StringSliceVar(&f.notifyOn, "notify-on", nil, "Replace the tiers that notify: APPROACHING, BREACHING")
	}
	addTreasuryTierFlags(cmd, &f.approaching, "approaching")
	addTreasuryTierFlags(cmd, &f.breaching, "breaching")
}

func newTreasuryMonitorCreateCmd() *op.Definition {
	var f treasuryMonitorFlags
	cmd := &op.Definition{
		Use:   "create",
		Short: "Create a wallet threshold monitor",
		Long: `Create a treasury monitor for one wallet currency.

Each tier is STATIC unless a --<tier>-template, --<tier>-predicate, or
--<tier>-param flag is given, which makes it DYNAMIC. STATIC tiers require an
explicit value (>= 0) and breaching must be strictly below approaching. A
DYNAMIC tier requires a predicate over the template's output columns and
$balance. Alerts default to enabled for both tiers, matching the Treasury UI.

Use --input FILE for a complete request body instead of flags.`,
		Example: `  bitwave treasury monitors create --wallet WALLET_ID --wallet-name "Ops ETH" \
    --network eth --address 0xabc... --currency-id 10 \
    --approaching 5 --breaching 2 --dry-run

  bitwave treasury monitors create --wallet WALLET_ID --currency-id 566847868622592 \
    --approaching 100 --breaching-template TEMPLATE_ID \
    --breaching-param party=validator::1220... \
    --breaching-predicate '$balance < $total_earned * 0.702' --yes`,
		Args: op.NoArgs,
		RunE: func(cmd *op.Call, _ []string) error {
			const operation = "create-treasury-monitor"
			body, err := buildTreasuryCreateBody(cmd, &f)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			orgID, err := resolveReportOrg(cmd.Context(), f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			request := treasuryRequest{method: http.MethodPost, path: treasuryPath(orgID, "monitoring-configs"), body: body}
			return runTreasuryMutation(cmd, f.transactionMutationFlags, orgID, operation, "create a treasury monitor", request, func(result json.RawMessage) string {
				return fmt.Sprintf("created treasury monitor %s\n", resultID(result))
			})
		},
	}
	addTreasuryMonitorBodyFlags(cmd, &f, true)
	return cmd
}

func buildTreasuryCreateBody(cmd *op.Call, f *treasuryMonitorFlags) (*treasuryCreateBody, error) {
	body := &treasuryCreateBody{}
	if f.input != "" {
		if changed := changedAny(cmd, f.bodyFlagNames()...); len(changed) > 0 {
			return nil, fmt.Errorf("use either --input or body flags, not both (got %s)", strings.Join(changed, ", "))
		}
		if err := readTreasuryInput(cmd, f.input, body); err != nil {
			return nil, err
		}
	} else {
		approaching, err := f.approaching.build(cmd)
		if err != nil {
			return nil, err
		}
		breaching, err := f.breaching.build(cmd)
		if err != nil {
			return nil, err
		}
		if approaching == nil {
			return nil, errors.New("an approaching threshold is required (--approaching VALUE, or --approaching-predicate for a dynamic tier)")
		}
		if breaching == nil {
			return nil, errors.New("a breaching threshold is required (--breaching VALUE, or --breaching-predicate for a dynamic tier)")
		}
		notifyOn := f.notifyOn
		if !cmd.Flags().Changed("notify-on") {
			notifyOn = treasuryDefaultNotify
		} else if len(notifyOn) == 0 {
			return nil, errors.New("--notify-on needs at least one of APPROACHING or BREACHING")
		}
		body = &treasuryCreateBody{
			WalletID: strings.TrimSpace(f.walletID), WalletName: f.walletName, Network: f.network, Address: f.address,
			CurrencyID: f.currencyID, CurrencyType: f.currencyType, AlertEnabled: f.alertEnabled, NotifyOn: notifyOn,
			Approaching: *approaching, Breaching: *breaching,
		}
	}
	if strings.TrimSpace(body.WalletID) == "" {
		return nil, errors.New("walletId is required (--wallet)")
	}
	if body.CurrencyID <= 0 {
		return nil, errors.New("currencyId is required and must be positive (--currency-id)")
	}
	currencyType, err := normalizeCurrencyType(body.CurrencyType)
	if err != nil {
		return nil, err
	}
	body.CurrencyType = currencyType
	if body.NotifyOn, err = normalizeNotifyOn(body.NotifyOn); err != nil {
		return nil, err
	}
	if err := validateTreasuryThresholds(&body.Approaching, &body.Breaching); err != nil {
		return nil, err
	}
	return body, nil
}

func newTreasuryMonitorUpdateCmd() *op.Definition {
	var f treasuryMonitorFlags
	cmd := &op.Definition{
		Use:   "update MONITOR_ID",
		Short: "Update a monitor's thresholds or notification settings",
		Long: `Update a treasury monitor. Only the fields you pass are sent.

Passing any flag for a tier replaces that whole tier (mode, value, and rule).
treasury-svc validates the resulting approaching/breaching pair; the CLI checks
the ordering rule locally only when both tiers are given. The wallet and
currency of a monitor cannot change; delete and recreate it instead.

Use --input FILE for a request body with any of approaching, breaching,
alertEnabled, and notifyOn.`,
		Example: `  bitwave treasury monitors update MONITOR_ID --approaching 8 --breaching 3 --dry-run
  bitwave treasury monitors update MONITOR_ID --alert-enabled=false --yes`,
		Args: op.ExactArgs(1),
		RunE: func(cmd *op.Call, args []string) error {
			const operation = "update-treasury-monitor"
			body, err := buildTreasuryUpdateBody(cmd, &f)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			orgID, err := resolveReportOrg(cmd.Context(), f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			request := treasuryRequest{method: http.MethodPut, path: treasuryPath(orgID, "monitoring-configs", args[0]), body: body}
			return runTreasuryMutation(cmd, f.transactionMutationFlags, orgID, operation, "update a treasury monitor", request, func(json.RawMessage) string {
				return fmt.Sprintf("updated treasury monitor %s\n", args[0])
			})
		},
	}
	addTreasuryMonitorBodyFlags(cmd, &f, false)
	return cmd
}

func buildTreasuryUpdateBody(cmd *op.Call, f *treasuryMonitorFlags) (*treasuryUpdateBody, error) {
	body := &treasuryUpdateBody{}
	if f.input != "" {
		names := append([]string{"alert-enabled", "notify-on"}, f.approaching.flagNames()...)
		if changed := changedAny(cmd, append(names, f.breaching.flagNames()...)...); len(changed) > 0 {
			return nil, fmt.Errorf("use either --input or body flags, not both (got %s)", strings.Join(changed, ", "))
		}
		if err := readTreasuryInput(cmd, f.input, body); err != nil {
			return nil, err
		}
	} else {
		var err error
		if body.Approaching, err = f.approaching.build(cmd); err != nil {
			return nil, err
		}
		if body.Breaching, err = f.breaching.build(cmd); err != nil {
			return nil, err
		}
		if cmd.Flags().Changed("alert-enabled") {
			enabled := f.alertEnabled
			body.AlertEnabled = &enabled
		}
		if cmd.Flags().Changed("notify-on") {
			body.NotifyOn = f.notifyOn
			if len(body.NotifyOn) == 0 {
				return nil, errors.New("--notify-on needs at least one of APPROACHING or BREACHING")
			}
		}
	}
	if body.Approaching == nil && body.Breaching == nil && body.AlertEnabled == nil && len(body.NotifyOn) == 0 {
		return nil, errors.New("nothing to update: pass a threshold, --alert-enabled, --notify-on, or --input")
	}
	var err error
	if body.NotifyOn, err = normalizeNotifyOn(body.NotifyOn); err != nil {
		return nil, err
	}
	if err := validateTreasuryThresholds(body.Approaching, body.Breaching); err != nil {
		return nil, err
	}
	return body, nil
}

func newTreasuryMonitorDeleteCmd() *op.Definition {
	var f transactionMutationFlags
	cmd := &op.Definition{
		Use:   "delete MONITOR_ID",
		Short: "Delete a monitor and resolve its open alert",
		Args:  op.ExactArgs(1),
		RunE: func(cmd *op.Call, args []string) error {
			const operation = "delete-treasury-monitor"
			orgID, err := resolveReportOrg(cmd.Context(), f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			request := treasuryRequest{method: http.MethodDelete, path: treasuryPath(orgID, "monitoring-configs", args[0])}
			return runTreasuryMutation(cmd, f, orgID, operation, "delete a treasury monitor", request, func(json.RawMessage) string {
				return fmt.Sprintf("deleted treasury monitor %s\n", args[0])
			})
		},
	}
	addMutationFlags(cmd, &f)
	return cmd
}

func newTreasuryMonitorReevaluateCmd() *op.Definition {
	var f transactionMutationFlags
	var all bool
	cmd := &op.Definition{
		Use:   "reevaluate [MONITOR_ID...]",
		Short: "Re-evaluate monitors now and persist their status",
		Long: `Re-evaluate monitors immediately instead of waiting for the next balance
update or scheduled check (up to 10 minutes).

treasury-svc has no dedicated evaluate endpoint. This command re-saves each
monitor's current approaching/breaching thresholds unchanged (PUT), which makes
treasury-svc evaluate it right away: dynamic tiers run their analytics
templates (billed query credits), and the resulting status is persisted and may
fire or resolve alerts. The Treasury UI's "Re-evaluate dynamic thresholds"
button runs the same templates but only updates the browser view.

Pass monitor IDs, or --all for every monitor with a DYNAMIC tier. --dry-run
reads the current monitors (GET only) to print the exact PUT requests.`,
		Args: op.ArbitraryArgs,
		RunE: func(cmd *op.Call, args []string) error {
			const operation = "reevaluate-treasury-monitors"
			if all == (len(args) > 0) {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("pass monitor IDs or --all (not both)"))
			}
			orgID, err := resolveReportOrg(cmd.Context(), f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			client := newReportsClient(cmd.Context(), orgID)
			monitors, err := loadTreasuryMonitors(cmd, client, orgID, args, all)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			requests := make([]treasuryRequest, 0, len(monitors))
			previews := make([]map[string]any, 0, len(monitors))
			for _, m := range monitors {
				approaching, breaching := m.Approaching, m.Breaching
				if err := validateTreasuryThresholds(&approaching, &breaching); err != nil {
					return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("monitor %s has invalid stored thresholds: %w", m.ID, err))
				}
				request := treasuryRequest{method: http.MethodPut, path: treasuryPath(orgID, "monitoring-configs", m.ID), body: treasuryUpdateBody{Approaching: &approaching, Breaching: &breaching}}
				preview, err := request.preview(client)
				if err != nil {
					return mutationError(cmd, operation, f.jsonOutput, err)
				}
				requests = append(requests, request)
				previews = append(previews, preview)
			}
			if f.dryRun {
				return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: operation, Organization: orgID, DryRun: true, Request: previews})
			}
			if !f.yes {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("refusing to re-evaluate treasury monitors without --yes (use --dry-run to preview)"))
			}
			results := make([]json.RawMessage, 0, len(requests))
			for i, request := range requests {
				result, err := request.send(cmd, client)
				if err != nil {
					return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("re-evaluate monitor %s (%d of %d; %d already re-evaluated): %w", monitors[i].ID, i+1, len(requests), i, err))
				}
				results = append(results, result)
			}
			var human strings.Builder
			if len(results) == 0 {
				human.WriteString("no monitors with dynamic thresholds to re-evaluate\n")
			}
			for _, raw := range results {
				var m treasuryMonitor
				_ = json.Unmarshal(raw, &m)
				status := m.LastStatus
				if status == "" {
					status = "PENDING"
				}
				fmt.Fprintf(&human, "re-evaluated %s (%s): %s\n", m.ID, firstNonEmpty(m.WalletName, m.WalletID), status)
			}
			return outputMutation(cmd, f.jsonOutput, mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: operation, Organization: orgID, Request: previews, Result: map[string]any{"count": len(results), "monitors": results}}, human.String())
		},
	}
	addMutationFlags(cmd, &f)
	cmd.Flags().BoolVar(&all, "all", false, "Re-evaluate every monitor that has a DYNAMIC tier")
	return cmd
}

func loadTreasuryMonitors(cmd *op.Call, client *orgreports.Client, orgID string, ids []string, all bool) ([]treasuryMonitor, error) {
	if all {
		data, err := client.RawRequest(cmd.Context(), orgreports.APIServiceCore, http.MethodGet, treasuryPath(orgID, "monitoring-configs"), nil)
		if err != nil {
			return nil, fmt.Errorf("list treasury monitors: %w", err)
		}
		var list []treasuryMonitor
		if err := json.Unmarshal(data, &list); err != nil {
			return nil, fmt.Errorf("unexpected monitoring-configs response: %w", err)
		}
		dynamic := make([]treasuryMonitor, 0, len(list))
		for _, m := range list {
			if m.dynamic() {
				dynamic = append(dynamic, m)
			}
		}
		return dynamic, nil
	}
	monitors := make([]treasuryMonitor, 0, len(ids))
	for _, id := range ids {
		data, err := client.RawRequest(cmd.Context(), orgreports.APIServiceCore, http.MethodGet, treasuryPath(orgID, "monitoring-configs", id), nil)
		if err != nil {
			return nil, fmt.Errorf("get treasury monitor %s: %w", id, err)
		}
		var m treasuryMonitor
		if err := json.Unmarshal(data, &m); err != nil || m.ID == "" {
			return nil, fmt.Errorf("unexpected monitoring-config response for %s", id)
		}
		monitors = append(monitors, m)
	}
	return monitors, nil
}

func newTreasuryAlertAckCmd() *op.Definition {
	var f transactionMutationFlags
	cmd := &op.Definition{
		Use:     "ack ALERT_ID",
		Aliases: []string{"acknowledge"},
		Short:   "Acknowledge an open treasury alert",
		Long:    "Mark an ACTIVE alert as seen by the caller. Resolved alerts cannot be acknowledged (409).",
		Args:    op.ExactArgs(1),
		RunE: func(cmd *op.Call, args []string) error {
			const operation = "acknowledge-treasury-alert"
			orgID, err := resolveReportOrg(cmd.Context(), f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			request := treasuryRequest{method: http.MethodPost, path: treasuryPath(orgID, "alerts", args[0], "acknowledge")}
			return runTreasuryMutation(cmd, f, orgID, operation, "acknowledge a treasury alert", request, func(json.RawMessage) string {
				return fmt.Sprintf("acknowledged treasury alert %s\n", args[0])
			})
		},
	}
	addMutationFlags(cmd, &f)
	return cmd
}

func newTreasuryChannelsSetCmd() *op.Definition {
	var f transactionMutationFlags
	var input, channel, target string
	var enabled bool
	cmd := &op.Definition{
		Use:   "set",
		Short: "Set one alert delivery channel",
		Long: `Upsert alert delivery channel settings. Channels not named are unchanged.

The stored target is replaced with --target, so pass the full value you want
kept even when only disabling a channel. EMAIL targets are comma-separated
recipient addresses; an enabled EMAIL channel needs at least one valid
address. SLACK (channel name) and WEBHOOK (URL) are stored but not delivered
yet. Use --input FILE for a {"channels":[...]} body with several channels.`,
		Example: `  bitwave treasury channels set --channel EMAIL --target "ops@example.com,cfo@example.com" --dry-run
  bitwave treasury channels set --channel EMAIL --target "ops@example.com" --enabled=false --yes`,
		Args: op.NoArgs,
		RunE: func(cmd *op.Call, _ []string) error {
			const operation = "set-treasury-alert-channels"
			body := treasuryChannelsBody{}
			if input != "" {
				if changed := changedAny(cmd, "channel", "target", "enabled"); len(changed) > 0 {
					return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("use either --input or channel flags, not both (got %s)", strings.Join(changed, ", ")))
				}
				if err := readTreasuryInput(cmd, input, &body); err != nil {
					return mutationError(cmd, operation, f.jsonOutput, err)
				}
			} else {
				if channel == "" {
					return mutationError(cmd, operation, f.jsonOutput, errors.New("--channel is required (EMAIL, SLACK, or WEBHOOK), or use --input"))
				}
				if !cmd.Flags().Changed("target") {
					return mutationError(cmd, operation, f.jsonOutput, errors.New("--target is required: the stored target is replaced by this value"))
				}
				body.Channels = []treasuryChannel{{Channel: channel, Enabled: enabled, Target: target}}
			}
			if err := validateTreasuryChannels(body.Channels); err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			orgID, err := resolveReportOrg(cmd.Context(), f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			request := treasuryRequest{method: http.MethodPut, path: treasuryPath(orgID, "alert-channels"), body: body}
			return runTreasuryMutation(cmd, f, orgID, operation, "change treasury alert channels", request, func(json.RawMessage) string {
				return fmt.Sprintf("updated %d treasury alert channel(s)\n", len(body.Channels))
			})
		},
	}
	addMutationFlags(cmd, &f)
	cmd.Flags().StringVarP(&input, "input", "i", "", `Complete {"channels":[...]} JSON file, or - for stdin`)
	cmd.Flags().StringVar(&channel, "channel", "", "Channel: EMAIL, SLACK, or WEBHOOK")
	cmd.Flags().StringVar(&target, "target", "", "Channel target: comma-separated emails, Slack channel, or webhook URL")
	cmd.Flags().BoolVar(&enabled, "enabled", true, "Enable the channel (--enabled=false disables it)")
	return cmd
}

// validateTreasuryChannels mirrors domain.AlertChannelSetting.Validate.
func validateTreasuryChannels(channels []treasuryChannel) error {
	if len(channels) == 0 {
		return errors.New("channels is required")
	}
	seen := map[string]bool{}
	for i := range channels {
		name, err := oneOf("channel", channels[i].Channel, treasuryChannels)
		if err != nil {
			return err
		}
		if seen[name] {
			return fmt.Errorf("channel %s is listed more than once", name)
		}
		seen[name] = true
		channels[i].Channel = name
		if channels[i].Enabled && name == "EMAIL" {
			recipients := strings.FieldsFunc(channels[i].Target, func(r rune) bool {
				return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
			})
			if len(recipients) == 0 {
				return errors.New("an enabled EMAIL channel requires at least one recipient in --target")
			}
			for _, recipient := range recipients {
				if !treasuryEmailPattern.MatchString(recipient) {
					return fmt.Errorf("%q is not an email address", recipient)
				}
			}
		}
	}
	return nil
}

func newTreasuryTemplateExecuteCmd() *op.Definition {
	var f transactionMutationFlags
	var params []string
	var costTier string
	var confirmLarge bool
	cmd := &op.Definition{
		Use:     "execute TEMPLATE_ID",
		Aliases: []string{"run"},
		Short:   "Run an analytics query template (billed) and print its result",
		Long: `Run an analytics-query-svc template with parameters, as the Treasury UI does
to preview a dynamic threshold. Executions consume the organization's query
credits (HTTP 402 when the budget is exhausted), so this requires --yes.

Result columns are the $variables available to a dynamic predicate. A query
larger than the service's confirmation threshold returns 409 with an estimate;
re-run with --confirm-large-query to accept it.`,
		Args: op.ExactArgs(1),
		RunE: func(cmd *op.Call, args []string) error {
			const operation = "execute-analytics-template"
			parameters, err := parseTreasuryParams("--param", params)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			body := map[string]any{"templateId": args[0], "parameters": parameters}
			if costTier != "" {
				tier, err := oneOf("--cost-tier", strings.ToLower(costTier), treasuryCostTiers)
				if err != nil {
					return mutationError(cmd, operation, f.jsonOutput, err)
				}
				body["costTier"] = tier
			}
			if confirmLarge {
				body["confirmLargeQuery"] = true
			}
			orgID, err := resolveReportOrg(cmd.Context(), f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			request := treasuryRequest{method: http.MethodPost, path: treasuryPath(orgID, "analytics", "queries", "execute"), body: body}
			return runTreasuryMutation(cmd, f, orgID, operation, "run a billed analytics query", request, func(result json.RawMessage) string {
				return formatTreasuryQueryResult(result)
			})
		},
	}
	addMutationFlags(cmd, &f)
	cmd.Flags().StringArrayVar(&params, "param", nil, "Template parameter key=value (repeatable; JSON literals such as 10, true, or \"10\" keep their type)")
	cmd.Flags().StringVar(&costTier, "cost-tier", "", "Optional cost tier override: small, medium, or large")
	cmd.Flags().BoolVar(&confirmLarge, "confirm-large-query", false, "Accept a query that exceeds the large-query threshold")
	return cmd
}

func formatTreasuryQueryResult(raw json.RawMessage) string {
	var response struct {
		Execution struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"execution"`
		Result *struct {
			Columns []struct {
				Name string `json:"name"`
			} `json:"columns"`
			Rows [][]any `json:"rows"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return string(raw) + "\n"
	}
	var out strings.Builder
	fmt.Fprintf(&out, "execution %s: %s\n", response.Execution.ID, response.Execution.Status)
	if response.Result == nil {
		return out.String()
	}
	w := tabwriter.NewWriter(&out, 0, 4, 2, ' ', 0)
	names := make([]string, 0, len(response.Result.Columns))
	for _, column := range response.Result.Columns {
		names = append(names, column.Name)
	}
	fmt.Fprintln(w, strings.Join(names, "\t"))
	for _, row := range response.Result.Rows {
		cells := make([]string, 0, len(row))
		for _, cell := range row {
			cells = append(cells, fmt.Sprint(cell))
		}
		fmt.Fprintln(w, strings.Join(cells, "\t"))
	}
	_ = w.Flush()
	return out.String()
}

// ---- Presentation helpers --------------------------------------------------

func describeTreasuryTier(t treasuryThreshold) string {
	if strings.EqualFold(t.Mode, treasuryModeDynamic) && t.Rule != nil {
		predicate := t.Rule.Predicate
		if predicate == "" && t.Rule.ResultColumn != "" {
			predicate = "$balance <= $" + t.Rule.ResultColumn
		}
		return "DYNAMIC(" + predicate + ")"
	}
	if t.Value == nil {
		return "-"
	}
	return formatTreasuryNumber(*t.Value)
}

func formatTreasuryNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
