package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/wavie/config"
	"github.com/bitwave-io/bitwave-cli/internal/wavie/shares"
	"github.com/bitwave-io/bitwave-cli/internal/wavie/workspaceshare"
)

func newShareCmd() *cobra.Command {
	var (
		to        string
		journalId string
		message   string
		ttlHours  int
		dryRun    bool
	)
	cmd := &cobra.Command{
		Use:   "share",
		Short: "Send a time-limited read-only link to a journal via email",
		Long: `Generate a tokenized URL recipients can use to view the named journal
read-only, and email the link to --to.

Auth:
  Local mode share is ANONYMOUS — no ` + "`wavie auth login`" + ` required. gl-svc
  accepts the zipped workspace upload from an unauthenticated client and
  delivers a magic-link invite. The recipient is the one who needs to be
  authenticated (they sign in via the magic link to adopt the workspace
  into their org).

  Cloud mode share DOES require auth: the server tokenizes the live journal
  on behalf of your org, so ` + "`wavie auth login`" + ` + ` + "`wavie org use`" + ` must already
  be set up.

What gets sent:
  Local mode  → zips the WHOLE workspace dir (all journals + accounts +
                prices) and uploads it. The recipient adopts it as a new
                workspace under their org.
  Cloud mode  → no payload; the server reads from the existing cloud
                workspace and emits a tokenized read-only link.

Examples:
  wavie share --to teammate@example.com
  wavie share --to teammate@example.com --message "May expenses for review"
  wavie share --to teammate@example.com --ttl 24       # link expires in 24h
  wavie share --to teammate@example.com --dry-run      # don't actually send`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if to == "" {
				return fmt.Errorf("--to is required")
			}
			cfg, dir, err := loadCwdConfig()
			if err != nil {
				return err
			}
			s, _, _, err := resolveStore(cmd.Context())
			if err != nil {
				return err
			}
			jId, err := resolveJournal(cmd.Context(), s, cfg, journalId)
			if err != nil {
				return err
			}

			if dryRun {
				fmt.Printf("DRY RUN: would POST share for journal %s to %s (ttl=%dh)\n",
					jId, to, ttlHours)
				return nil
			}

			if cfg.Mode == config.ModeLocal {
				// Local-mode share = upload the entire workspace (zip) so the
				// recipient can adopt it into their org. The whole workspace
				// goes — gl-svc has no way to know which journal entries the
				// shared journal references across the other ledger files,
				// and shipping the whole thing keeps balance/price/account
				// validation intact server-side. The recipient picks the
				// adopted workspace name; we do not pass --journal through.
				_ = jId
				wc := workspaceshare.New(resolveGLBaseURL(), nil)
				resp, err := wc.UploadAndShare(cmd.Context(), dir, to, message)
				if err != nil {
					return fmt.Errorf("upload workspace share: %w", err)
				}
				if resp.EmailDelivered {
					fmt.Printf("Invite emailed to %s\n", to)
				} else {
					fmt.Printf("Share recorded for %s (email delivery not confirmed)\n", to)
				}
				fmt.Printf("  Workspace: %s\n", resp.WorkspaceId)
				fmt.Printf("  Recipient: %s\n", resp.RecipientId)
				return nil
			}
			if cfg.OrgId == "" {
				return fmt.Errorf(".wavie.toml is missing org_id for cloud share")
			}

			req := shares.CreateRequest{
				RecipientEmail: to,
				Message:        message,
				TTLHours:       ttlHours,
			}

			orgId := cfg.OrgId
			c := shares.New(resolveGLBaseURL(), orgId, makeOrgTokenResolver(orgId))
			resp, err := c.Create(cmd.Context(), jId, req)
			if err != nil {
				return fmt.Errorf("create share: %w", err)
			}
			fmt.Printf("Shared journal %s with %s\n", jId, to)
			fmt.Printf("  URL:     %s\n", resp.URL)
			fmt.Printf("  Expires: %s\n", resp.ExpiresAt.Local().Format("2006-01-02 15:04 MST"))
			fmt.Printf("  ShareId: %s\n", resp.ShareId)
			if resp.EmailFailure != "" {
				fmt.Fprintf(os.Stderr, "  Warning: email send failed (%s); the link is still active.\n", resp.EmailFailure)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "Recipient email address (required)")
	cmd.Flags().StringVar(&journalId, "journal", "", "Journal id (defaults to .wavie.toml default_journal)")
	cmd.Flags().StringVar(&message, "message", "", "Optional message to include in the email")
	cmd.Flags().IntVar(&ttlHours, "ttl", 168, "Lifetime of the share link in hours (default 7 days)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would be sent without contacting gl-svc")
	return cmd
}

func newSharesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shares",
		Short: "List or revoke journal shares",
	}
	cmd.AddCommand(newSharesListCmd())
	cmd.AddCommand(newSharesRevokeCmd())
	return cmd
}

func newSharesListCmd() *cobra.Command {
	var journalId string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List shares for a journal",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, _, err := loadCwdConfig()
			if err != nil {
				return err
			}
			if cfg.OrgId == "" {
				return fmt.Errorf(".wavie.toml is missing org_id")
			}
			s, _, _, err := resolveStore(cmd.Context())
			if err != nil {
				return err
			}
			jId, err := resolveJournal(cmd.Context(), s, cfg, journalId)
			if err != nil {
				return err
			}
			c := shares.New(resolveGLBaseURL(), cfg.OrgId, makeOrgTokenResolver(cfg.OrgId))
			list, err := c.List(cmd.Context(), jId)
			if err != nil {
				return err
			}
			if len(list) == 0 {
				fmt.Println("(no shares)")
				return nil
			}
			for _, s := range list {
				fmt.Printf("%-36s  %-9s  %-32s  expires %s\n",
					s.ShareId, s.Status, s.RecipientEmail, s.ExpiresAt.Local().Format("2006-01-02 15:04"))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&journalId, "journal", "", "Journal id (defaults to .wavie.toml default_journal)")
	return cmd
}

func newSharesRevokeCmd() *cobra.Command {
	var journalId string
	cmd := &cobra.Command{
		Use:   "revoke <shareId>",
		Short: "Revoke an active share link",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadCwdConfig()
			if err != nil {
				return err
			}
			if cfg.OrgId == "" {
				return fmt.Errorf(".wavie.toml is missing org_id")
			}
			s, _, _, err := resolveStore(cmd.Context())
			if err != nil {
				return err
			}
			jId, err := resolveJournal(cmd.Context(), s, cfg, journalId)
			if err != nil {
				return err
			}
			c := shares.New(resolveGLBaseURL(), cfg.OrgId, makeOrgTokenResolver(cfg.OrgId))
			out, err := c.Revoke(cmd.Context(), jId, args[0])
			if err != nil {
				return err
			}
			fmt.Printf("Revoked share %s (status=%s)\n", out.ShareId, out.Status)
			return nil
		},
	}
	cmd.Flags().StringVar(&journalId, "journal", "", "Journal id (defaults to .wavie.toml default_journal)")
	return cmd
}
