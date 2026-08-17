package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	bitwavecli "github.com/bitwave-io/bitwave-cli/sdk"
	"github.com/spf13/cobra"
)

const (
	defaultWavieBridgeAddress = "127.0.0.1:7314"
	wavieBridgeProtocol       = "wavie.v1"
	wavieBridgeHeader         = "X-Bitwave-Local-Bridge"
	maxBridgeResults          = 256
)

var defaultWavieBridgeOrigins = []string{
	"https://app3.bitwave.io",
	"https://staging-app3.bitwave.io",
	"https://bitwave-staging3.web.app",
	"http://bitwave.localhost:5173",
	"http://localhost:5173",
	"http://127.0.0.1:5173",
}

type wavieBridgeStatus struct {
	Connected       bool            `json:"connected"`
	ProtocolVersion string          `json:"protocolVersion"`
	CLIVersion      string          `json:"cliVersion"`
	LocalRoot       string          `json:"localRoot"`
	Tool            wavieBridgeTool `json:"tool"`
}

type wavieBridgeTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Safety      string          `json:"safety"`
}

type wavieBridgeExecuteRequest struct {
	RequestID      string   `json:"requestId"`
	OrganizationID string   `json:"organizationId,omitempty"`
	Args           []string `json:"args"`
	Reason         string   `json:"reason"`
	Approved       bool     `json:"approved"`
}

type wavieBridgeApprovalRequired struct {
	RequiresApproval bool   `json:"requiresApproval"`
	Risk             string `json:"risk"`
}

type wavieBridge struct {
	executable string
	localRoot  string
	origins    map[string]struct{}
	sem        chan struct{}

	mu          sync.Mutex
	results     map[string]bitwavecli.CommandResult
	resultOrder []string
}

func newWavieLocalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "wavie",
		Short:  "Connect the local Bitwave CLI to Wavie",
		Hidden: true,
		Long: `Connect the installed Bitwave CLI to Wavie in the Bitwave web app.

The local bridge listens only on the loopback interface. Wavie can propose
structured Bitwave CLI commands. Read-only commands run automatically; changes
must be approved in the web app. No shell interpreter is exposed.`,
	}
	cmd.AddCommand(newWavieConnectCmd())
	cmd.AddCommand(newWavieServiceCmd())
	return cmd
}

func newWavieConnectCmd() *cobra.Command {
	var listenAddress string
	var extraOrigins []string
	var serviceMode bool
	cmd := &cobra.Command{
		Use:   "connect",
		Short: "Run the local bridge used by the Wavie web chat",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			suppressWavieServiceMaintenance = true
			if err := validateLoopbackAddress(listenAddress); err != nil {
				return err
			}
			executable, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolve Bitwave executable: %w", err)
			}
			localRoot, err := wavieBridgeWorkingDirectory(serviceMode)
			if err != nil {
				return err
			}
			origins := append([]string{}, defaultWavieBridgeOrigins...)
			origins = append(origins, extraOrigins...)
			bridge := newWavieBridge(executable, localRoot, origins)
			listener, err := net.Listen("tcp", listenAddress)
			if err != nil {
				return fmt.Errorf("start Wavie local bridge on %s: %w", listenAddress, err)
			}
			defer func() { _ = listener.Close() }()

			server := &http.Server{
				Handler:           bridge,
				ReadHeaderTimeout: 5 * time.Second,
				ReadTimeout:       15 * time.Second,
				IdleTimeout:       60 * time.Second,
			}
			if !serviceMode {
				fmt.Fprintf(cmd.OutOrStdout(), "Wavie local CLI connected at http://%s\n", listener.Addr().String())
				fmt.Fprintln(cmd.OutOrStdout(), "Keep this process running while using Wavie. Commands still require approval in the Bitwave web app.")
			}
			if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&listenAddress, "listen", defaultWavieBridgeAddress, "Loopback address for the local Wavie bridge")
	cmd.Flags().StringSliceVar(&extraOrigins, "allow-origin", nil, "Additional exact web origin allowed to call the bridge")
	cmd.Flags().BoolVar(&serviceMode, "service", false, "Run as the automatically managed background service")
	_ = cmd.Flags().MarkHidden("service")
	return cmd
}

func wavieBridgeWorkingDirectory(serviceMode bool) (string, error) {
	if serviceMode {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return home, nil
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve local working directory: %w", err)
	}
	return workingDirectory, nil
}

func validateLoopbackAddress(address string) error {
	host, _, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return fmt.Errorf("invalid Wavie bridge address %q: %w", address, err)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("refusing non-loopback Wavie bridge address %q", address)
	}
	return nil
}

func newWavieBridge(executable, localRoot string, origins []string) *wavieBridge {
	allowed := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		if trimmed := strings.TrimRight(strings.TrimSpace(origin), "/"); trimmed != "" {
			allowed[trimmed] = struct{}{}
		}
	}
	return &wavieBridge{
		executable: executable,
		localRoot:  localRoot,
		origins:    allowed,
		sem:        make(chan struct{}, 1),
		results:    make(map[string]bitwavecli.CommandResult),
	}
}

func (b *wavieBridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !b.allowOrigin(w, r) {
		http.Error(w, "origin is not allowed by the Wavie local bridge", http.StatusForbidden)
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/status":
		b.writeJSON(w, http.StatusOK, wavieBridgeStatus{
			Connected: true, ProtocolVersion: wavieBridgeProtocol, CLIVersion: Version, LocalRoot: b.localRoot,
			Tool: wavieBridgeTool{
				Name:        bitwavecli.ToolName,
				Description: bitwavecli.ToolDescription,
				InputSchema: bitwavecli.ToolInputSchema,
				Safety:      "write",
			},
		})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/execute":
		b.execute(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (b *wavieBridge) allowOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimRight(strings.TrimSpace(r.Header.Get("Origin")), "/")
	if origin == "" {
		return true
	}
	if _, ok := b.origins[origin]; !ok {
		return false
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Vary", "Origin")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, "+wavieBridgeHeader)
	w.Header().Set("Access-Control-Allow-Private-Network", "true")
	return true
}

func (b *wavieBridge) execute(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get(wavieBridgeHeader) != wavieBridgeProtocol {
		http.Error(w, "missing or invalid Wavie local bridge protocol header", http.StatusBadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer func() { _ = r.Body.Close() }()
	var input wavieBridgeExecuteRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.OrganizationID = strings.TrimSpace(input.OrganizationID)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.RequestID == "" || input.Reason == "" {
		http.Error(w, "requestId and reason are required", http.StatusBadRequest)
		return
	}
	if err := bitwavecli.ValidateArgs(input.Args); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if risk := classifyBitwaveArgs(input.Args); risk != "read" && !input.Approved {
		b.writeJSON(w, http.StatusPreconditionRequired, wavieBridgeApprovalRequired{
			RequiresApproval: true,
			Risk:             risk,
		})
		return
	}
	if cached, ok := b.cachedResult(input.RequestID); ok {
		b.writeJSON(w, http.StatusOK, cached)
		return
	}

	select {
	case b.sem <- struct{}{}:
		defer func() { <-b.sem }()
	case <-r.Context().Done():
		http.Error(w, r.Context().Err().Error(), http.StatusRequestTimeout)
		return
	}
	// Check again after waiting: another identical request may have completed.
	if cached, ok := b.cachedResult(input.RequestID); ok {
		b.writeJSON(w, http.StatusOK, cached)
		return
	}
	result := bitwavecli.Execute(r.Context(), b.executable, b.localRoot, input.Args, input.OrganizationID)
	b.cacheResult(input.RequestID, result)
	b.writeJSON(w, http.StatusOK, result)
}

// classifyBitwaveArgs is the local authority for approval policy. The model's
// description of a command is intentionally not trusted for this decision.
// Unknown commands fail closed and require approval.
func classifyBitwaveArgs(args []string) string {
	if len(args) == 0 {
		return "read"
	}
	for _, arg := range args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "--help", "-h", "help", "--version":
			return "read"
		}
	}
	words := commandWords(args)
	if len(words) == 0 {
		return "write"
	}
	switch words[0] {
	case "version", "status", "bal", "balance", "reg", "register", "print", "accounts", "contacts", "commodities", "equity", "cleared", "csv", "stats", "report":
		return "read"
	case "org":
		if len(words) > 1 && (words[1] == "current" || words[1] == "list") {
			return "read"
		}
	case "transaction", "transactions", "txn":
		if len(words) > 1 {
			switch words[1] {
			case "get", "search", "summary", "categorization-options":
				return "read"
			}
		}
	case "rule", "rules":
		if len(words) > 1 && (words[1] == "get" || words[1] == "list" || words[1] == "validate") {
			return "read"
		}
	case "inventory":
		if len(words) > 1 && (words[1] == "list" || words[1] == "updates") {
			return "read"
		}
	}
	if isHighRiskCommand(words) {
		return "destructive"
	}
	return "write"
}

func commandWords(args []string) []string {
	words := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := strings.ToLower(strings.TrimSpace(args[i]))
		if arg == "" {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			// Global flags accepted before the command. Values for these flags are
			// skipped so they cannot be mistaken for a command word.
			if (arg == "--token" || arg == "--auth-url") && i+1 < len(args) {
				i++
			}
			continue
		}
		words = append(words, arg)
	}
	return words
}

func isHighRiskCommand(words []string) bool {
	if len(words) == 0 {
		return false
	}
	if words[0] == "migrate" {
		return true
	}
	return len(words) > 1 && words[0] == "wallets" && words[1] == "send"
}

func (b *wavieBridge) cachedResult(requestID string) (bitwavecli.CommandResult, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	result, ok := b.results[requestID]
	return result, ok
}

func (b *wavieBridge) cacheResult(requestID string, result bitwavecli.CommandResult) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.results[requestID]; exists {
		return
	}
	b.results[requestID] = result
	b.resultOrder = append(b.resultOrder, requestID)
	if len(b.resultOrder) > maxBridgeResults {
		oldest := b.resultOrder[0]
		b.resultOrder = b.resultOrder[1:]
		delete(b.results, oldest)
	}
}

func (b *wavieBridge) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
