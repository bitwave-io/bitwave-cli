// Package sdk exposes the Bitwave CLI as a single structured tool for agent
// harnesses. Consumers import this package and invoke the CLI with argv; no
// HTTP bridge or callback service is involved.
package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	bitwavecmd "github.com/bitwave-io/bitwave-cli/internal/bitwave/cmd"
)

const (
	ToolName        = "run_bitwave_cli"
	ToolProvider    = "bitwave-cli"
	ToolDescription = "Run the complete Bitwave CLI with structured parameters. Pass command arguments as an array without the `bitwave` executable name; omit `args` or pass an empty array to return whole-CLI help. Use `info --json` to discover the full live command tree. Select this tool from ordinary user intent whenever the user asks to inspect or change Bitwave data; never require them to mention the CLI. Prefer `--json` for structured results. Organization platform workflows include users, wallets, tokens, transactions, rules, accounting, inventory, pricing, reports, diagnostics, and authenticated API requests. For organization balances use `report balance`; `bal` reads a separate local plain-text ledger. Arguments execute directly without a shell."
	maxOutput       = 1 << 20
)

var ToolInputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "args": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Arguments passed to bitwave, excluding the executable name. Omit or pass an empty array to return bitwave --help. Use separate array elements; shell syntax is not supported.",
      "default": []
    }
  },
  "additionalProperties": false
}`)

type CommandResult struct {
	Command   []string `json:"command"`
	Directory string   `json:"directory"`
	ExitCode  int      `json:"exitCode"`
	Stdout    string   `json:"stdout,omitempty"`
	Stderr    string   `json:"stderr,omitempty"`
	Truncated bool     `json:"truncated,omitempty"`
}

// ExecuteOptions supplies invocation-scoped context without relying on a
// user's persisted CLI configuration. Environment changes and working-directory
// changes are serialized and restored before ExecuteWithOptions returns.
type ExecuteOptions struct {
	Args             []string
	WorkingDirectory string
	OrganizationID   string
	Token            string
	AgentToken       string
}

var executeMu sync.Mutex

func ValidateArgs(args []string) error {
	for _, arg := range args {
		if strings.ContainsRune(arg, 0) {
			return errors.New("refusing a Bitwave argument containing a NUL byte")
		}
	}
	return nil
}

func NormalizeArgs(args []string) []string {
	if len(args) == 0 {
		return []string{"--help"}
	}
	return append([]string(nil), args...)
}

// ExecuteWithOptions runs one Bitwave command in process. The mutex is
// intentional: Cobra's command package retains a small amount of flag state,
// and invocation-scoped environment/cwd values must not bleed across agents.
func ExecuteWithOptions(ctx context.Context, options ExecuteOptions) CommandResult {
	executeMu.Lock()
	defer executeMu.Unlock()

	args := NormalizeArgs(options.Args)
	stdout := &limitedBuffer{limit: maxOutput}
	stderr := &limitedBuffer{limit: maxOutput}

	restoreEnv := setInvocationEnv(map[string]string{
		"BITWAVE_QUIET":       "1",
		"BITWAVE_ORG_ID":      strings.TrimSpace(options.OrganizationID),
		"BITWAVE_TOKEN":       strings.TrimSpace(options.Token),
		"BITWAVE_AGENT_TOKEN": strings.TrimSpace(options.AgentToken),
	})
	defer restoreEnv()

	cwd, _ := os.Getwd()
	if options.WorkingDirectory != "" {
		if err := os.Chdir(options.WorkingDirectory); err != nil {
			return CommandResult{Command: append([]string{"bitwave"}, args...), Directory: options.WorkingDirectory, ExitCode: 1, Stderr: err.Error()}
		}
		defer func() { _ = os.Chdir(cwd) }()
	}

	root := bitwavecmd.NewRootCmd()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetContext(ctx)
	_, err := root.ExecuteC()
	exitCode := 0
	if err != nil {
		exitCode = 1
		if errors.Is(err, context.Canceled) {
			exitCode = 130
		}
		_, _ = fmt.Fprintln(stderr, err)
	}
	directory, _ := os.Getwd()
	return CommandResult{
		Command:   append([]string{"bitwave"}, args...),
		Directory: directory,
		ExitCode:  exitCode,
		Stdout:    stdout.String(),
		Stderr:    stderr.String(),
		Truncated: stdout.truncated || stderr.truncated,
	}
}

// Execute is the compact API for callers that only need org scope.
func Execute(ctx context.Context, args []string, organizationID string) CommandResult {
	return ExecuteWithOptions(ctx, ExecuteOptions{Args: args, OrganizationID: organizationID})
}

func setInvocationEnv(values map[string]string) func() {
	type previousValue struct {
		value string
		set   bool
	}
	previous := make(map[string]previousValue, len(values))
	for key, value := range values {
		prior, set := os.LookupEnv(key)
		previous[key] = previousValue{value: prior, set: set}
		if value == "" {
			_ = os.Unsetenv(key)
		} else {
			_ = os.Setenv(key, value)
		}
	}
	return func() {
		for key, prior := range previous {
			if prior.set {
				_ = os.Setenv(key, prior.value)
			} else {
				_ = os.Unsetenv(key)
			}
		}
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
		b.truncated = b.truncated || originalLength > 0
		return originalLength, nil
	}
	if len(p) > remaining {
		_, _ = b.buffer.Write(p[:remaining])
		b.truncated = true
		return originalLength, nil
	}
	_, _ = b.buffer.Write(p)
	return originalLength, nil
}

func (b *limitedBuffer) String() string { return b.buffer.String() }
