package main

import (
	"fmt"
	"os"

	"github.com/bitwave-io/bitwave-cli/internal/bwx/cmd"
)

func main() {
	root := cmd.NewRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
