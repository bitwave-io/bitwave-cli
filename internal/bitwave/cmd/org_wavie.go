package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

func newOrgWavieCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wavie",
		Short: "Create and operate Wavie chat sessions",
		Long: `Send messages to Bitwave's Wavie service through its versioned session API.

Wavie sessions are scoped to one organization. Create a session, send one or
more messages, then read its transcript. These commands use the dedicated
Wavie gateway rather than the removed legacy /conversations API.`,
	}
	cmd.AddCommand(newOrgWavieSessionCmd())
	cmd.AddCommand(newOrgWavieMessageCmd())
	cmd.AddCommand(newOrgWavieTranscriptCmd())
	cmd.AddCommand(newOrgWavieInterruptCmd())
	return cmd
}

func newOrgWavieSessionCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "session", Short: "Manage Wavie sessions"}
	cmd.AddCommand(newOrgWavieSessionCreateCmd())
	return cmd
}

func newOrgWavieSessionCreateCmd() *cobra.Command {
	var f transactionMutationFlags
	var model string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an organization-scoped Wavie session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			operation := "wavie-session-create"
			orgID, err := resolveReportOrg(f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			request := orgreports.CreateWavieSessionRequest{
				Capabilities: orgreports.WavieCapabilities{ClientKind: "cli", ClientVersion: orgreports.WavieProtocolVersion, Tools: []any{}},
				Model:        strings.TrimSpace(model),
			}
			preview := map[string]any{"method": "POST", "path": fmt.Sprintf("/v3/orgs/%s/wavie/sessions", orgID), "body": request}
			if f.dryRun {
				return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: operation, Organization: orgID, DryRun: true, Request: preview})
			}
			if !f.yes {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("refusing to create a Wavie session without --yes (use --dry-run to preview)"))
			}
			client := orgreports.New(resolveWavieBaseURL(), makeOrgTokenResolver(orgID))
			session, err := client.CreateWavieSession(cmd.Context(), orgID, request.Model)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("create Wavie session: %w", err))
			}
			return outputMutation(cmd, f.jsonOutput, mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: operation, Organization: orgID, Result: session}, fmt.Sprintf("Wavie session %s created (model: %s).\n", session.SessionID, session.Model))
		},
	}
	addMutationFlags(cmd, &f)
	cmd.Flags().StringVar(&model, "model", "", "Optional model override; omit to use the Wavie service default")
	return cmd
}

func newOrgWavieMessageCmd() *cobra.Command {
	var f transactionMutationFlags
	cmd := &cobra.Command{
		Use:   "message SESSION_ID MESSAGE...",
		Short: "Send a message to an existing Wavie session",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			operation := "wavie-message"
			orgID, err := resolveReportOrg(f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			sessionID := strings.TrimSpace(args[0])
			message := strings.TrimSpace(strings.Join(args[1:], " "))
			if message == "" {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("message cannot be empty"))
			}
			body := map[string]string{"message": message}
			preview := map[string]any{"method": "POST", "path": fmt.Sprintf("/v3/orgs/%s/wavie/sessions/%s/messages", orgID, sessionID), "body": body}
			if f.dryRun {
				return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: operation, Organization: orgID, DryRun: true, Request: preview})
			}
			if !f.yes {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("refusing to send a Wavie message without --yes (use --dry-run to preview)"))
			}
			client := orgreports.New(resolveWavieBaseURL(), makeOrgTokenResolver(orgID))
			turn, err := client.PostWavieMessage(cmd.Context(), orgID, sessionID, message)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("send Wavie message: %w", err))
			}
			result := map[string]any{"sessionId": sessionID, "turnId": turn.TurnID}
			return outputMutation(cmd, f.jsonOutput, mutationEnvelope{SchemaVersion: "1", Status: "accepted", Operation: operation, Organization: orgID, Result: result}, fmt.Sprintf("Wavie accepted turn %s in session %s.\n", turn.TurnID, sessionID))
		},
	}
	addMutationFlags(cmd, &f)
	return cmd
}

func newOrgWavieTranscriptCmd() *cobra.Command {
	var orgID string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "transcript SESSION_ID",
		Short: "Read a Wavie session transcript",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedOrg, err := resolveReportOrg(orgID)
			if err != nil {
				return err
			}
			client := orgreports.New(resolveWavieBaseURL(), makeOrgTokenResolver(resolvedOrg))
			transcript, err := client.WavieTranscript(cmd.Context(), resolvedOrg, args[0])
			if err != nil {
				return fmt.Errorf("read Wavie transcript: %w", err)
			}
			if jsonOutput {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "organization": resolvedOrg, "sessionId": args[0], "transcript": transcript})
			}
			for _, entry := range transcript.Entries {
				if entry.Text != "" {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", entry.Kind, entry.Text); err != nil {
						return err
					}
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func newOrgWavieInterruptCmd() *cobra.Command {
	var f transactionMutationFlags
	cmd := &cobra.Command{
		Use:   "interrupt SESSION_ID",
		Short: "Interrupt the active turn in a Wavie session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			operation := "wavie-interrupt"
			orgID, err := resolveReportOrg(f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			sessionID := strings.TrimSpace(args[0])
			preview := map[string]any{"method": "POST", "path": fmt.Sprintf("/v3/orgs/%s/wavie/sessions/%s/interrupt", orgID, sessionID), "body": map[string]any{}}
			if f.dryRun {
				return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: operation, Organization: orgID, DryRun: true, Request: preview})
			}
			if !f.yes {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("refusing to interrupt a Wavie session without --yes (use --dry-run to preview)"))
			}
			client := orgreports.New(resolveWavieBaseURL(), makeOrgTokenResolver(orgID))
			if err := client.InterruptWavieSession(cmd.Context(), orgID, sessionID); err != nil {
				return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("interrupt Wavie session: %w", err))
			}
			result := map[string]any{"sessionId": sessionID}
			return outputMutation(cmd, f.jsonOutput, mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: operation, Organization: orgID, Result: result}, fmt.Sprintf("Interrupted Wavie session %s.\n", sessionID))
		},
	}
	addMutationFlags(cmd, &f)
	return cmd
}
