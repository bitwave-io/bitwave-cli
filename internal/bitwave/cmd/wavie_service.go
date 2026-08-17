package cmd

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	wavieServiceLabel       = "io.bitwave.cli.wavie"
	wavieLinuxServiceName   = "bitwave-wavie.service"
	wavieWindowsTaskName    = "Bitwave Wavie Bridge"
	wavieServiceDescription = "Bitwave Wavie local CLI bridge"
	wavieServiceAttemptFile = "wavie-service-attempt"
	wavieServiceRetryAfter  = 24 * time.Hour
)

var suppressWavieServiceMaintenance bool

type wavieServicePaths struct {
	definition string
	log        string
}

func newWavieServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage Wavie's automatic local CLI connection",
		Long: `Manage the per-user background service that connects Wavie to this CLI.

Normal users do not need to run these commands. Supported installers register
and start the service automatically; these commands are available for support,
development, and recovery. The service listens on loopback only, and commands
that change Bitwave state require approval in the web app. Read-only commands
run automatically.`,
	}
	cmd.AddCommand(
		newWavieServiceActionCmd("install", "Install and start the Wavie background service", installWavieService),
		newWavieServiceActionCmd("start", "Start the Wavie background service", startWavieService),
		newWavieServiceActionCmd("stop", "Stop the Wavie background service", stopWavieService),
		newWavieServiceActionCmd("uninstall", "Stop and remove the Wavie background service", uninstallWavieService),
		newWavieServiceStatusCmd(),
	)
	return cmd
}

func newWavieServiceActionCmd(use, short string, action func(string, string) error) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			suppressWavieServiceMaintenance = true
			executable, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolve Bitwave executable: %w", err)
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home directory: %w", err)
			}
			if err := action(executable, home); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Wavie local CLI service %s.\n", servicePastTense(use))
			return nil
		},
	}
}

// MaybeEnsureWavieService makes non-package installation paths, including
// `go install`, converge on the same zero-setup Wavie experience. Packaged
// installers register the service immediately; this is the best-effort safety
// net after the first invocation of a release binary. It never delays the
// command the user just ran.
func MaybeEnsureWavieService() {
	if suppressWavieServiceMaintenance || wavieBridgeIsRunning() {
		return
	}
	if strings.Contains(strings.ToLower(Version), "dev") || os.Getenv("BITWAVE_NO_WAVIE_SERVICE") == "1" {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil || !shouldAttemptWavieService(home, time.Now()) {
		return
	}
	_ = installWavieService(executable, home)
}

func shouldAttemptWavieService(home string, now time.Time) bool {
	stateDirectory := filepath.Join(home, ".bitwave")
	marker := filepath.Join(stateDirectory, wavieServiceAttemptFile)
	if info, err := os.Stat(marker); err == nil && now.Sub(info.ModTime()) < wavieServiceRetryAfter {
		return false
	}
	if err := os.MkdirAll(stateDirectory, 0o700); err != nil {
		return false
	}
	return os.WriteFile(marker, []byte(now.UTC().Format(time.RFC3339)+"\n"), 0o600) == nil
}

func servicePastTense(action string) string {
	switch action {
	case "install":
		return "installed and started"
	case "start":
		return "started"
	case "stop":
		return "stopped"
	case "uninstall":
		return "uninstalled"
	default:
		return action
	}
}

func newWavieServiceStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check whether Wavie's local CLI connection is running",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			suppressWavieServiceMaintenance = true
			if !wavieBridgeIsRunning() {
				return errors.New("Wavie local CLI service is not running")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Wavie local CLI connected at http://%s\n", defaultWavieBridgeAddress)
			return nil
		},
	}
}

func installWavieService(executable, home string) error {
	paths, err := wavieServiceFilePaths(runtime.GOOS, home)
	if err != nil {
		return err
	}
	if paths.log != "" {
		if err := os.MkdirAll(filepath.Dir(paths.log), 0o700); err != nil {
			return fmt.Errorf("create Wavie log directory: %w", err)
		}
	}

	switch runtime.GOOS {
	case "darwin":
		if err := writeServiceDefinition(paths.definition, renderLaunchAgent(executable, home, paths.log)); err != nil {
			return err
		}
		domain := fmt.Sprintf("gui/%d", currentUserID())
		_ = runServiceCommand("launchctl", "bootout", domain+"/"+wavieServiceLabel)
		return runServiceCommand("launchctl", "bootstrap", domain, paths.definition)
	case "linux":
		if err := writeServiceDefinition(paths.definition, renderSystemdUserService(executable, home)); err != nil {
			return err
		}
		if err := runServiceCommand("systemctl", "--user", "daemon-reload"); err != nil {
			return err
		}
		return runServiceCommand("systemctl", "--user", "enable", "--now", wavieLinuxServiceName)
	case "windows":
		command, err := windowsTaskCommand(executable)
		if err != nil {
			return err
		}
		if err := runServiceCommand("schtasks.exe", "/Create", "/F", "/SC", "ONLOGON", "/TN", wavieWindowsTaskName, "/TR", command); err != nil {
			return err
		}
		return runServiceCommand("schtasks.exe", "/Run", "/TN", wavieWindowsTaskName)
	default:
		return fmt.Errorf("automatic Wavie connection is not supported on %s", runtime.GOOS)
	}
}

func startWavieService(_, home string) error {
	switch runtime.GOOS {
	case "darwin":
		paths, err := wavieServiceFilePaths(runtime.GOOS, home)
		if err != nil {
			return err
		}
		domain := fmt.Sprintf("gui/%d", currentUserID())
		if err := runServiceCommand("launchctl", "bootstrap", domain, paths.definition); err == nil {
			return nil
		}
		return runServiceCommand("launchctl", "kickstart", "-k", domain+"/"+wavieServiceLabel)
	case "linux":
		return runServiceCommand("systemctl", "--user", "start", wavieLinuxServiceName)
	case "windows":
		return runServiceCommand("schtasks.exe", "/Run", "/TN", wavieWindowsTaskName)
	default:
		return fmt.Errorf("automatic Wavie connection is not supported on %s", runtime.GOOS)
	}
}

func stopWavieService(_, _ string) error {
	switch runtime.GOOS {
	case "darwin":
		return runServiceCommand("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", currentUserID(), wavieServiceLabel))
	case "linux":
		return runServiceCommand("systemctl", "--user", "stop", wavieLinuxServiceName)
	case "windows":
		return runServiceCommand("schtasks.exe", "/End", "/TN", wavieWindowsTaskName)
	default:
		return fmt.Errorf("automatic Wavie connection is not supported on %s", runtime.GOOS)
	}
}

func uninstallWavieService(_, home string) error {
	paths, err := wavieServiceFilePaths(runtime.GOOS, home)
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case "darwin":
		domain := fmt.Sprintf("gui/%d", currentUserID())
		_ = runServiceCommand("launchctl", "bootout", domain+"/"+wavieServiceLabel)
		return removeServiceDefinition(paths.definition)
	case "linux":
		_ = runServiceCommand("systemctl", "--user", "disable", "--now", wavieLinuxServiceName)
		if err := removeServiceDefinition(paths.definition); err != nil {
			return err
		}
		return runServiceCommand("systemctl", "--user", "daemon-reload")
	case "windows":
		_ = runServiceCommand("schtasks.exe", "/End", "/TN", wavieWindowsTaskName)
		return runServiceCommand("schtasks.exe", "/Delete", "/F", "/TN", wavieWindowsTaskName)
	default:
		return fmt.Errorf("automatic Wavie connection is not supported on %s", runtime.GOOS)
	}
}

func wavieServiceFilePaths(goos, home string) (wavieServicePaths, error) {
	if strings.TrimSpace(home) == "" {
		return wavieServicePaths{}, errors.New("home directory is required")
	}
	switch goos {
	case "darwin":
		return wavieServicePaths{
			definition: filepath.Join(home, "Library", "LaunchAgents", wavieServiceLabel+".plist"),
			log:        filepath.Join(home, ".bitwave", "logs", "wavie-bridge.log"),
		}, nil
	case "linux":
		return wavieServicePaths{definition: filepath.Join(home, ".config", "systemd", "user", wavieLinuxServiceName)}, nil
	case "windows":
		return wavieServicePaths{}, nil
	default:
		return wavieServicePaths{}, fmt.Errorf("automatic Wavie connection is not supported on %s", goos)
	}
}

func writeServiceDefinition(path, contents string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create Wavie service directory: %w", err)
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, []byte(contents), 0o600); err != nil {
		return fmt.Errorf("write Wavie service definition: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("install Wavie service definition: %w", err)
	}
	return nil
}

func removeServiceDefinition(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove Wavie service definition: %w", err)
	}
	return nil
}

func renderLaunchAgent(executable, home, logPath string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>--quiet</string>
    <string>wavie</string>
    <string>connect</string>
    <string>--service</string>
  </array>
  <key>WorkingDirectory</key><string>%s</string>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>10</integer>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, xmlText(wavieServiceLabel), xmlText(executable), xmlText(home), xmlText(logPath), xmlText(logPath))
}

func renderSystemdUserService(executable, home string) string {
	return fmt.Sprintf(`[Unit]
Description=%s

[Service]
Type=simple
ExecStart=%s --quiet wavie connect --service
WorkingDirectory=%s
Restart=on-failure
RestartSec=10

[Install]
WantedBy=default.target
`, wavieServiceDescription, systemdQuote(executable), systemdQuote(home))
}

func xmlText(value string) string {
	var output bytes.Buffer
	_ = xml.EscapeText(&output, []byte(value))
	return output.String()
}

func systemdQuote(value string) string {
	return strconv.Quote(strings.ReplaceAll(value, "%", "%%"))
}

func windowsTaskCommand(executable string) (string, error) {
	if strings.Contains(executable, `"`) {
		return "", errors.New("Bitwave executable path contains an unsupported quote character")
	}
	return fmt.Sprintf(`"%s" --quiet wavie connect --service`, executable), nil
}

func runServiceCommand(name string, args ...string) error {
	command := exec.Command(name, args...)
	output, err := command.CombinedOutput()
	if err == nil {
		return nil
	}
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return fmt.Errorf("%s: %w", name, err)
	}
	return fmt.Errorf("%s: %w: %s", name, err, detail)
}

func wavieBridgeIsRunning() bool {
	connection, err := net.DialTimeout("tcp", defaultWavieBridgeAddress, 250*time.Millisecond)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}
