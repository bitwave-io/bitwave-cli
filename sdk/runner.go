// Package sdk exposes the Bitwave CLI as a single structured tool for agent
// harnesses. Consumers import this package and invoke the CLI with argv; no
// HTTP bridge or callback service is involved.
package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	ToolName        = "run_bitwave_cli"
	ToolProvider    = "bitwave-cli"
	ToolDescription = "Run the Bitwave CLI with structured parameters. Pass command arguments as an array without the `bitwave` executable name; omit `args` or pass an empty array to return `bitwave --help`. Select this tool from ordinary user intent whenever the user asks to inspect or change Bitwave data; never require them to mention the CLI. Prefer `--json` for structured results. For organization balances use `report balance`; `bal` reads a separate local plain-text ledger. Arguments execute directly without a shell."
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

// Execute invokes one Bitwave command using an exact executable path and argv.
// organizationID is injected only for this child process; no process-global
// environment or CLI context is mutated.
func Execute(ctx context.Context, executable, cwd string, args []string, organizationID string) CommandResult {
	args = NormalizeArgs(args)
	stdout := &limitedBuffer{limit: maxOutput}
	stderr := &limitedBuffer{limit: maxOutput}
	local := exec.CommandContext(ctx, executable, args...)
	local.Dir = cwd
	local.Env = append(os.Environ(), "BITWAVE_QUIET=1")
	if organizationID = strings.TrimSpace(organizationID); organizationID != "" {
		local.Env = append(local.Env, "BITWAVE_ORG_ID="+organizationID)
	}
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
	return CommandResult{
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
