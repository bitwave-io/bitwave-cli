//go:build !windows

package cmd

import "os"

func currentUserID() int {
	return os.Getuid()
}
