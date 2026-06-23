package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/auth"
)

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate with Bitwave",
	}
	cmd.AddCommand(newAuthLoginCmd())
	cmd.AddCommand(newAuthLogoutCmd())
	cmd.AddCommand(newAuthStatusCmd())
	cmd.AddCommand(newAuthDelegateCmd())
	cmd.AddCommand(newAuthResumeCmd())
	cmd.AddCommand(newAuthAgentCmd())
	return cmd
}

func newAuthLoginCmd() *cobra.Command {
	var clientID, clientSecret string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in via PKCE browser flow (or client credentials)",
		Long: `Sign in to Bitwave.

Default: browser-based PKCE flow.

For headless / CI use, prefer the BITWAVE_AGENT_TOKEN env var (issued by
` + "`bwx auth agent create`" + `). Falling back to OAuth client credentials is
also supported via --client-id / --client-secret (also reads BITWAVE_CLIENT_ID
and BITWAVE_CLIENT_SECRET).

Tokens land in ~/.bw/credentials.json and are auto-refreshed.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if clientID == "" {
				clientID = os.Getenv("BITWAVE_CLIENT_ID")
			}
			if clientSecret == "" {
				clientSecret = os.Getenv("BITWAVE_CLIENT_SECRET")
			}
			if clientID != "" || clientSecret != "" {
				if clientID == "" || clientSecret == "" {
					return fmt.Errorf("both --client-id and --client-secret are required for client credentials login")
				}
				creds, err := auth.ClientCredentialsLogin(resolveAuthURL(), clientID, clientSecret)
				if err != nil {
					return err
				}
				if err := auth.SaveCredentials(creds); err != nil {
					return err
				}
				auth.PrintLoginSuccess("", creds.ExpiresAt)
				return nil
			}
			return auth.Login(resolveAuthURL())
		},
	}
	cmd.Flags().StringVar(&clientID, "client-id", "", "OAuth client ID (env: BITWAVE_CLIENT_ID)")
	cmd.Flags().StringVar(&clientSecret, "client-secret", "", "OAuth client secret (env: BITWAVE_CLIENT_SECRET)")
	return cmd
}

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Clear stored credentials",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return auth.Logout(resolveAuthURL())
		},
	}
}

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current identity, token source, and scopes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if v := os.Getenv("BITWAVE_AGENT_TOKEN"); v != "" {
				fmt.Println("Identity: agent (BITWAVE_AGENT_TOKEN env)")
				fmt.Println("Source:   environment")
				return nil
			}
			if v := os.Getenv("BITWAVE_TOKEN"); v != "" {
				fmt.Println("Identity: bearer token (BITWAVE_TOKEN env)")
				fmt.Println("Source:   environment")
				_ = v
				return nil
			}
			return auth.Status()
		},
	}
}

// newAuthDelegateCmd: server endpoint not yet built. Stub with a clear error
// pointing at what would happen.
func newAuthDelegateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delegate <email>",
		Short: "Request delegated access from a user (spin-waits on email approval)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("auth delegate is not yet implemented (server-side delegation flow is pending). Email that would be contacted: %s", args[0])
		},
	}
}

func newAuthResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume",
		Short: "Resume a pending login or delegation flow",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return fmt.Errorf("auth resume is not yet implemented (no pending-flow store on this client)")
		},
	}
}

func newAuthAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage well-known agent identities (issued tokens)",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "create --name <n> [--workspace <id>]",
		Short: "Issue an agent token (org admin)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return fmt.Errorf("auth agent create is not yet implemented (server-side issuance pending)")
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List agent tokens issued in the active org",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return fmt.Errorf("auth agent list is not yet implemented")
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoke an agent token",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return fmt.Errorf("auth agent revoke is not yet implemented")
		},
	})
	return cmd
}
