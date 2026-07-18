package cmd

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/update"
)

func newUpgradeCmd() *cobra.Command {
	var checkOnly bool
	c := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade bitwave to the latest release",
		Long: `Check for a newer bitwave release and install it.

The upgrade respects how bitwave was installed:
  - npm installs re-run  npm install -g bitwave@latest
  - Homebrew installs re-run  brew upgrade bitwave
  - direct installs (curl | sh, GitHub download) self-update in place after
    verifying the release checksum

Use --check to only report whether an update exists.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()
			latest, err := update.Refresh(ctx)
			if err != nil {
				return fmt.Errorf("checking latest release: %w", err)
			}
			current := strings.TrimPrefix(Version, "v")
			if !update.IsNewer(latest, current) {
				fmt.Fprintf(cmd.OutOrStdout(), "bitwave v%s is up to date (latest release: %s)\n", current, latest)
				return nil
			}
			if checkOnly {
				fmt.Fprintf(cmd.OutOrStdout(), "update available: %s (current v%s)\n", latest, current)
				return nil
			}

			exe, err := os.Executable()
			if err != nil {
				return err
			}
			if resolved, err := filepath.EvalSymlinks(exe); err == nil {
				exe = resolved
			}

			switch method := update.DetectInstallMethod(exe); method {
			case update.MethodNpm:
				fmt.Fprintf(cmd.ErrOrStderr(), "installed via npm — running: npm install -g bitwave@latest\n")
				return runPassthrough(cmd, "npm", "install", "-g", "bitwave@latest")
			case update.MethodBrew:
				// brew's auto-update is throttled (24h by default), so an
				// explicit update is required or the tap may not know about
				// the release this command just detected.
				fmt.Fprintf(cmd.ErrOrStderr(), "installed via Homebrew — running: brew update && brew upgrade bitwave\n")
				if err := runPassthrough(cmd, "brew", "update", "--quiet"); err != nil {
					return err
				}
				return runPassthrough(cmd, "brew", "upgrade", "bitwave")
			default:
				fmt.Fprintf(cmd.ErrOrStderr(), "self-updating %s -> %s\n", current, latest)
				if err := selfUpdate(ctx, exe, latest); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "upgraded to bitwave %s\n", latest)
				return nil
			}
		},
	}
	c.Flags().BoolVar(&checkOnly, "check", false, "Only report whether an update is available")
	return c
}

func runPassthrough(cmd *cobra.Command, name string, args ...string) error {
	e := exec.CommandContext(cmd.Context(), name, args...)
	e.Stdin = os.Stdin
	e.Stdout = cmd.OutOrStdout()
	e.Stderr = cmd.ErrOrStderr()
	return e.Run()
}

// selfUpdate downloads the release archive for this platform, verifies its
// sha256 against the release's checksums.txt, and atomically replaces the
// running binary.
func selfUpdate(ctx context.Context, exePath, tag string) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("self-update is not supported on Windows — use `npm install -g bitwave@latest` or download the new release zip from https://github.com/bitwave-io/bitwave-cli/releases")
	}
	bare := strings.TrimPrefix(tag, "v")
	archive := fmt.Sprintf("bitwave_%s_%s_%s.tar.gz", bare, runtime.GOOS, runtime.GOARCH)
	base := fmt.Sprintf("https://github.com/bitwave-io/bitwave-cli/releases/download/%s", tag)

	archiveBytes, err := fetch(ctx, base+"/"+archive)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", archive, err)
	}
	sums, err := fetch(ctx, base+"/checksums.txt")
	if err != nil {
		return fmt.Errorf("downloading checksums.txt: %w", err)
	}
	if err := verifyChecksum(archiveBytes, sums, archive); err != nil {
		return err
	}
	bin, err := extractBinary(archiveBytes, "bitwave")
	if err != nil {
		return err
	}

	// Write next to the current binary so the final rename is atomic
	// (same filesystem), then swap.
	dir := filepath.Dir(exePath)
	tmp, err := os.CreateTemp(dir, ".bitwave-upgrade-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s (try re-running with appropriate permissions): %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, exePath); err != nil {
		return fmt.Errorf("replacing %s: %w", exePath, err)
	}
	return nil
}

func fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

func verifyChecksum(data, checksums []byte, name string) error {
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == name {
			if fields[0] == got {
				return nil
			}
			return fmt.Errorf("checksum mismatch for %s", name)
		}
	}
	return fmt.Errorf("%s not found in checksums.txt", name)
}

func extractBinary(archive []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(strings.NewReader(string(archive)))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(hdr.Name) == name && hdr.Typeflag == tar.TypeReg {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("binary %q not found in archive", name)
}
