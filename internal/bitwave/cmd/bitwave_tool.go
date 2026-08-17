package cmd

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
	wavieLocalToolName = "run_bitwave_cli"
	maxLocalToolOutput = 1 << 20
)

// wavieLocalToolSchema is deliberately one operation with one optional input.
// The Wavie client supplies organization scope separately and an empty input
// is the CLI's normal discovery path: bitwave --help.
var wavieLocalToolSchema = json.RawMessage(`{
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

type localCommandResult struct {
	Command   []string `json:"command"`
	Directory string   `json:"directory"`
	ExitCode  int      `json:"exitCode"`
	Stdout    string   `json:"stdout,omitempty"`
	Stderr    string   `json:"stderr,omitempty"`
	Truncated bool     `json:"truncated,omitempty"`
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

func normalizeBitwaveArgs(args []string) []string {
	if len(args) == 0 {
		return []string{"--help"}
	}
	return append([]string(nil), args...)
}

func executeBitwaveCommandForOrg(ctx context.Context, executable, cwd string, args []string, organizationID string) localCommandResult {
	args = normalizeBitwaveArgs(args)
	stdout := &limitedBuffer{limit: maxLocalToolOutput}
	stderr := &limitedBuffer{limit: maxLocalToolOutput}
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
