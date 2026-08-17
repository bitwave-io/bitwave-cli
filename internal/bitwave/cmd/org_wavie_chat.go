package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

const (
	wavieLocalToolName = "run_bitwave_cli"
	maxLocalToolOutput = 1 << 20
)

var wavieLocalToolSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "args": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Arguments passed to bitwave, excluding the executable name. Use separate array elements; shell syntax is not supported."
    },
    "reason": {
      "type": "string",
      "description": "A short user-facing explanation of why this command is needed."
    }
  },
  "required": ["args", "reason"],
  "additionalProperties": false
}`)

type wavieLocalToolInput struct {
	Args   []string `json:"args"`
	Reason string   `json:"reason"`
}

type wavieToolRequestPayload struct {
	TurnID     string          `json:"turnId"`
	ToolCallID string          `json:"toolCallId"`
	Name       string          `json:"name"`
	Input      json.RawMessage `json:"input"`
}

type wavieTextPayload struct {
	TurnID string `json:"turnId"`
	Text   string `json:"text"`
}

type wavieTurnCompletePayload struct {
	TurnID     string `json:"turnId"`
	StopReason string `json:"stopReason"`
}

type wavieErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type localCommandResult struct {
	Command   []string `json:"command"`
	Directory string   `json:"directory"`
	ExitCode  int      `json:"exitCode"`
	Stdout    string   `json:"stdout,omitempty"`
	Stderr    string   `json:"stderr,omitempty"`
	Truncated bool     `json:"truncated,omitempty"`
}

func newOrgWavieChatCmd() *cobra.Command {
	var orgID string
	var model string
	cmd := &cobra.Command{
		Use:   "chat [MESSAGE...]",
		Short: "Chat with Wavie and approve local Bitwave CLI operations",
		Long: `Start a Wavie session that can request commands from the local Bitwave CLI.

Wavie receives one structured tool: run_bitwave_cli. Each proposed command is
shown with its reason and working directory and runs only after you approve it.
The tool invokes this Bitwave executable directly; it does not evaluate shell
syntax, pipes, redirects, or arbitrary programs.

Pass a message to run one turn and exit. With no message, chat stays open until
you enter /exit or press Ctrl-D.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedOrg, err := resolveReportOrg(orgID)
			if err != nil {
				return err
			}
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("resolve local working directory: %w", err)
			}
			executable, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolve Bitwave executable: %w", err)
			}

			client := orgreports.New(resolveWavieBaseURL(), makeOrgTokenResolver(resolvedOrg))
			request := orgreports.CreateWavieSessionRequest{
				Capabilities: orgreports.WavieCapabilities{
					ClientKind:    "cli",
					ClientVersion: orgreports.WavieProtocolVersion,
					LocalRoot:     cwd,
					Tools: []orgreports.WavieClientTool{{
						Name:        wavieLocalToolName,
						Description: "Run the local Bitwave CLI using the user's existing authentication. Pass argv only (without the word bitwave); no shell syntax. Use --help to discover commands and prefer --json for structured results. The local client shows the exact command and requires user approval before every execution.",
						InputSchema: wavieLocalToolSchema,
						Safety:      "write",
					}},
				},
				Model: strings.TrimSpace(model),
			}
			session, err := client.CreateWavieSessionWithCapabilities(cmd.Context(), resolvedOrg, request)
			if err != nil {
				return fmt.Errorf("create local Wavie session: %w", err)
			}

			streamCtx, cancel := context.WithCancel(cmd.Context())
			defer cancel()
			events := make(chan orgreports.WavieEvent, 64)
			streamErrors := make(chan error, 1)
			go func() {
				err := client.StreamWavieSession(streamCtx, resolvedOrg, session.SessionID, "", func(event orgreports.WavieEvent) error {
					select {
					case events <- event:
						return nil
					case <-streamCtx.Done():
						return streamCtx.Err()
					}
				})
				streamErrors <- err
			}()
			if err := waitForWavieReady(streamCtx, events, streamErrors); err != nil {
				return err
			}

			reader := bufio.NewReader(cmd.InOrStdin())
			fmt.Fprintf(cmd.ErrOrStderr(), "Wavie session %s connected to org %s. Local commands require approval.\n", session.SessionID, resolvedOrg)
			if len(args) > 0 {
				message := strings.TrimSpace(strings.Join(args, " "))
				if message == "" {
					return errors.New("message cannot be empty")
				}
				return runWavieChatTurn(cmd, client, reader, executable, cwd, resolvedOrg, session.SessionID, message, events, streamErrors)
			}

			for {
				fmt.Fprint(cmd.ErrOrStderr(), "you> ")
				line, readErr := reader.ReadString('\n')
				message := strings.TrimSpace(line)
				if message == "/exit" || message == "/quit" {
					return nil
				}
				if message != "" {
					if err := runWavieChatTurn(cmd, client, reader, executable, cwd, resolvedOrg, session.SessionID, message, events, streamErrors); err != nil {
						return err
					}
				}
				if errors.Is(readErr, io.EOF) {
					return nil
				}
				if readErr != nil {
					return readErr
				}
			}
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().StringVar(&model, "model", "", "Optional model override; omit to use the Wavie service default")
	return cmd
}

func waitForWavieReady(ctx context.Context, events <-chan orgreports.WavieEvent, streamErrors <-chan error) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-streamErrors:
			if err == nil {
				return errors.New("Wavie stream closed before session.ready")
			}
			return fmt.Errorf("attach Wavie stream: %w", err)
		case event := <-events:
			if event.Event == "session.ready" {
				return nil
			}
			if event.Event == "error" {
				return wavieEventError(event)
			}
		}
	}
}

func runWavieChatTurn(cmd *cobra.Command, client *orgreports.Client, reader *bufio.Reader, executable, cwd, orgID, sessionID, message string, events <-chan orgreports.WavieEvent, streamErrors <-chan error) error {
	turn, err := client.PostWavieMessage(cmd.Context(), orgID, sessionID, message)
	if err != nil {
		return fmt.Errorf("send Wavie message: %w", err)
	}
	printedText := false
	for {
		select {
		case <-cmd.Context().Done():
			return cmd.Context().Err()
		case streamErr := <-streamErrors:
			if streamErr == nil {
				return errors.New("Wavie stream closed before turn completed")
			}
			return fmt.Errorf("Wavie stream failed: %w", streamErr)
		case event := <-events:
			switch event.Event {
			case "text.delta":
				var payload wavieTextPayload
				if json.Unmarshal(event.Data, &payload) == nil && payload.TurnID == turn.TurnID {
					if !printedText {
						fmt.Fprint(cmd.OutOrStdout(), "wavie> ")
						printedText = true
					}
					fmt.Fprint(cmd.OutOrStdout(), payload.Text)
				}
			case "tool.client.request":
				var payload wavieToolRequestPayload
				if err := json.Unmarshal(event.Data, &payload); err != nil {
					return fmt.Errorf("decode Wavie client tool request: %w", err)
				}
				if payload.TurnID != turn.TurnID {
					continue
				}
				result := handleWavieLocalTool(cmd.Context(), reader, cmd.ErrOrStderr(), executable, cwd, payload)
				if _, err := client.PostWavieToolResult(cmd.Context(), orgID, sessionID, result); err != nil {
					return fmt.Errorf("return local tool result: %w", err)
				}
			case "turn.complete":
				var payload wavieTurnCompletePayload
				if json.Unmarshal(event.Data, &payload) == nil && payload.TurnID == turn.TurnID {
					if printedText {
						fmt.Fprintln(cmd.OutOrStdout())
					}
					return nil
				}
			case "error":
				return wavieEventError(event)
			}
		}
	}
}

func wavieEventError(event orgreports.WavieEvent) error {
	var payload wavieErrorPayload
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return fmt.Errorf("Wavie stream error: %s", strings.TrimSpace(string(event.Data)))
	}
	if payload.Code == "" {
		return errors.New(payload.Message)
	}
	return fmt.Errorf("Wavie stream error %s: %s", payload.Code, payload.Message)
}

func handleWavieLocalTool(ctx context.Context, reader *bufio.Reader, prompt io.Writer, executable, cwd string, request wavieToolRequestPayload) orgreports.WavieToolResult {
	result := orgreports.WavieToolResult{ToolCallID: request.ToolCallID}
	if request.Name != wavieLocalToolName {
		result.Status = "denied"
		result.Content = "The local client does not expose this tool."
		return result
	}
	var input wavieLocalToolInput
	decoder := json.NewDecoder(bytes.NewReader(request.Input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		result.Status = "error"
		result.Content = "Invalid run_bitwave_cli input: " + err.Error()
		return result
	}
	if err := validateBitwaveArgs(input.Args); err != nil {
		result.Status = "denied"
		result.Content = err.Error()
		return result
	}
	if strings.TrimSpace(input.Reason) == "" {
		result.Status = "error"
		result.Content = "run_bitwave_cli requires a non-empty reason."
		return result
	}

	command := append([]string{"bitwave"}, input.Args...)
	fmt.Fprintf(prompt, "\nWavie requests a local command.\nReason: %s\nDirectory: %s\nCommand: %s\nApprove once? [y/N] ", strings.TrimSpace(input.Reason), cwd, renderCommand(command))
	answer, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		result.Status = "denied"
		result.Content = "Approval could not be read: " + err.Error()
		return result
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		fmt.Fprintln(prompt, "Denied.")
		result.Status = "denied"
		result.Content = "The user denied this local command."
		return result
	}

	fmt.Fprintln(prompt, "Running...")
	commandResult := executeBitwaveCommand(ctx, executable, cwd, input.Args)
	data, marshalErr := json.Marshal(commandResult)
	if marshalErr == nil {
		result.Data = data
	}
	if commandResult.ExitCode == 0 {
		result.Status = "ok"
		result.Content = strings.TrimSpace(commandResult.Stdout)
		if result.Content == "" {
			result.Content = "Bitwave command completed successfully."
		}
		if commandResult.Truncated {
			result.Content += "\n[Local command output was truncated at 1 MiB.]"
		}
		return result
	}
	result.Status = "error"
	result.Content = strings.TrimSpace(commandResult.Stderr)
	if result.Content == "" {
		result.Content = fmt.Sprintf("Bitwave command exited with status %d.", commandResult.ExitCode)
	}
	if commandResult.Truncated {
		result.Content += "\n[Local command output was truncated at 1 MiB.]"
	}
	return result
}

func validateBitwaveArgs(args []string) error {
	for _, arg := range args {
		if strings.ContainsRune(arg, 0) {
			return errors.New("refusing a Bitwave argument containing a NUL byte")
		}
		if strings.EqualFold(strings.TrimSpace(arg), "wavie") {
			return errors.New("refusing a recursive Bitwave Wavie command")
		}
	}
	return nil
}

func executeBitwaveCommand(ctx context.Context, executable, cwd string, args []string) localCommandResult {
	stdout := &limitedBuffer{limit: maxLocalToolOutput}
	stderr := &limitedBuffer{limit: maxLocalToolOutput}
	local := exec.CommandContext(ctx, executable, args...)
	local.Dir = cwd
	local.Env = append(os.Environ(), "BITWAVE_QUIET=1")
	local.Stdout = stdout
	local.Stderr = stderr
	err := local.Run()
	exitCode := 0
	if err != nil {
		exitCode = -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else if errors.Is(err, context.Canceled) {
			exitCode = 130
		}
	}
	return localCommandResult{
		Command:   append([]string{filepath.Base(executable)}, args...),
		Directory: cwd,
		ExitCode:  exitCode,
		Stdout:    stdout.String(),
		Stderr:    stderr.String(),
		Truncated: stdout.truncated || stderr.truncated,
	}
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	originalLength := len(p)
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.truncated = true
		return originalLength, nil
	}
	if len(p) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	_, _ = b.buffer.Write(p)
	return originalLength, nil
}

func (b *limitedBuffer) String() string { return b.buffer.String() }

func renderCommand(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		if arg != "" && strings.IndexFunc(arg, func(r rune) bool {
			return r == ' ' || r == '\t' || r == '\n' || r == '\'' || r == '"' || r == '\\'
		}) == -1 {
			quoted[i] = arg
			continue
		}
		quoted[i] = strconv.Quote(arg)
	}
	return strings.Join(quoted, " ")
}
