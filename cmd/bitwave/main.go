package main

import (
	"fmt"
	"os"
	"time"

	"github.com/bitwave-io/bitwave-cli/internal/bitwave/cmd"
	"github.com/bitwave-io/bitwave-cli/internal/telemetry"
)

func main() {
	root := cmd.NewRootCmd()
	start := time.Now()
	// ExecuteC (not Execute) so the deepest executed command is known even on
	// failure — cobra skips PersistentPostRun hooks when RunE errors, so
	// telemetry capture has to wrap the whole invocation.
	executed, err := root.ExecuteC()
	telemetry.RecordCommand(cmd.Version, executed, err, time.Since(start))
	telemetry.MaybeFlush(cmd.Version, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
