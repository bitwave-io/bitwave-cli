package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/auth"
	"github.com/bitwave-io/bitwave-cli/internal/bitwave/config"
	"github.com/bitwave-io/bitwave-cli/internal/orgctx"
	"github.com/bitwave-io/bitwave-cli/internal/update"
)

// quietFlag is set by --quiet (and respects BITWAVE_QUIET=1) to suppress the
// pre-run status banner.
var quietFlag bool

// bannerSkipCommands is the set of command paths where the banner is
// inappropriate: bootstrap commands that may run before a workspace exists
// or before auth is wired up, and pure read-only meta commands. A path
// matches if it equals an entry or if an entry is a strict ancestor.
var bannerSkipCommands = map[string]bool{
	"bitwave help":       true,
	"bitwave completion": true,
	"bitwave version":    true,
	"bitwave init":       true,
	"bitwave auth":       true,
	"bitwave status":     true,
}

// printStatusBanner emits a single-line workspace/identity hint to stderr
// so agents can see the operating context without an extra command.
// Honors --quiet and BITWAVE_QUIET=1.
func printStatusBanner(cmd *cobra.Command) {
	if quietFlag || os.Getenv("BITWAVE_QUIET") == "1" {
		return
	}
	path := cmd.CommandPath()
	// Skip the bare root (e.g. `bitwave --help`) — it has no useful target.
	if path == "bitwave" {
		return
	}
	// Skip exact matches or strict descendants of listed commands.
	for prefix := range bannerSkipCommands {
		if path == prefix || strings.HasPrefix(path, prefix+" ") {
			return
		}
	}
	_, _ = fmt.Fprintln(cmd.ErrOrStderr(), describeStatus())
	// Cached-only check — never a network wait. The cache refreshes after
	// commands finish (see PersistentPostRun on the root command).
	if notice, ok := update.CachedNotice(Version); ok {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), notice)
	}
}

// describeStatus returns the single-line banner string. Exposed so the
// `bitwave status` command can reuse the same formatter.
func describeStatus() string {
	return fmt.Sprintf("bitwave: %s | %s | %s",
		describeWorkspace(),
		describeOrg(),
		describeIdentity(),
	)
}

func describeWorkspace() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "workspace=?"
	}
	dir, err := config.Find(cwd)
	if err != nil {
		if errors.Is(err, config.ErrNotAWorkspace) {
			return "workspace=none (run `bitwave init`)"
		}
		return "workspace=?"
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return fmt.Sprintf("workspace=%s (unreadable)", dir)
	}
	mode := string(cfg.Mode)
	if mode == "" {
		mode = "local"
	}
	return fmt.Sprintf("workspace=%s (%s)", dir, mode)
}

func describeOrg() string {
	a, err := orgctx.Load()
	if err != nil || a == nil || a.OrgID == "" {
		return "org=none"
	}
	if a.OrgName != "" {
		return fmt.Sprintf("org=%s", a.OrgName)
	}
	return fmt.Sprintf("org=%s", a.OrgID)
}

func describeIdentity() string {
	if os.Getenv("BITWAVE_AGENT_TOKEN") != "" {
		return "identity=agent-token (env)"
	}
	if tokenFlag != "" {
		return "identity=bearer-token (flag)"
	}
	if os.Getenv("BITWAVE_TOKEN") != "" {
		return "identity=bearer-token (env)"
	}
	creds, err := auth.LoadCredentials()
	if err != nil || creds == nil {
		return "identity=anonymous (no `bitwave auth login` — fine for local mode)"
	}
	email := auth.ExtractEmailFromIDToken(creds.IDToken)
	if email == "" {
		email = "logged-in"
	}
	if creds.IsExpired() {
		return fmt.Sprintf("identity=%s (token expired, will refresh)", email)
	}
	return fmt.Sprintf("identity=%s", email)
}
