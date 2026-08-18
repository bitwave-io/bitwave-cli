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
	clientToolServiceLabel       = "io.bitwave.cli.client-tools"
	clientToolLinuxServiceName   = "bitwave-client-tools.service"
	clientToolWindowsTaskName    = "Bitwave Client Tools"
	clientToolServiceDescription = "Bitwave local client-tool transport"
	clientToolServiceAttemptFile = "client-tools-service-attempt"
	clientToolServiceRetryAfter  = 24 * time.Hour
)

var suppressClientToolServiceMaintenance bool

type clientToolServicePaths struct {
	definition string
	log        string
}

func newClientToolServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage the local Bitwave client-tool transport",
		Long: `Manage the per-user service that exposes the CLI to approved local clients.

Normal users do not need to run these commands. Supported installers register
and start the service automatically; these commands are available for support,
development, and recovery. The service listens on loopback only, and commands
that change Bitwave state require approval in the web app. Read-only commands
run automatically.`,
	}
	cmd.AddCommand(
		newClientToolServiceActionCmd("install", "Install and start the client-tool service", installClientToolService),
		newClientToolServiceActionCmd("start", "Start the client-tool service", startClientToolService),
		newClientToolServiceActionCmd("stop", "Stop the client-tool service", stopClientToolService),
		newClientToolServiceActionCmd("uninstall", "Stop and remove the client-tool service", uninstallClientToolService),
		newClientToolServiceStatusCmd(),
	)
	return cmd
}

func newClientToolServiceActionCmd(use, short string, action func(string, string) error) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			suppressClientToolServiceMaintenance = true
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
			fmt.Fprintf(cmd.OutOrStdout(), "Bitwave client-tool service %s.\n", servicePastTense(use))
			return nil
		},
	}
}

// MaybeEnsureClientToolService makes non-package installation paths, including
// `go install`, converge on the same zero-setup client-tool experience. Packaged
// installers register the service immediately; this is the best-effort safety
// net after the first invocation of a release binary. It never delays the
// command the user just ran.
func MaybeEnsureClientToolService() {
	if suppressClientToolServiceMaintenance || clientToolBridgeIsRunning() {
		return
	}
	if strings.Contains(strings.ToLower(Version), "dev") || os.Getenv("BITWAVE_NO_CLIENT_TOOLS_SERVICE") == "1" {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil || !shouldAttemptClientToolService(home, time.Now()) {
		return
	}
	_ = installClientToolService(executable, home)
}

func shouldAttemptClientToolService(home string, now time.Time) bool {
	stateDirectory := filepath.Join(home, ".bitwave")
	marker := filepath.Join(stateDirectory, clientToolServiceAttemptFile)
	if info, err := os.Stat(marker); err == nil && now.Sub(info.ModTime()) < clientToolServiceRetryAfter {
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

func newClientToolServiceStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check whether the local client-tool service is running",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			suppressClientToolServiceMaintenance = true
			if !clientToolBridgeIsRunning() {
				return errors.New("Bitwave client-tool service is not running")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Bitwave client tools available at http://%s\n", defaultClientToolBridgeAddress)
			return nil
		},
	}
}

func installClientToolService(executable, home string) error {
	paths, err := clientToolServiceFilePaths(runtime.GOOS, home)
	if err != nil {
		return err
	}
	if paths.log != "" {
		if err := os.MkdirAll(filepath.Dir(paths.log), 0o700); err != nil {
			return fmt.Errorf("create client-tool log directory: %w", err)
		}
	}

	switch runtime.GOOS {
	case "darwin":
		if err := writeServiceDefinition(paths.definition, renderLaunchAgent(executable, home, paths.log)); err != nil {
			return err
		}
		domain := fmt.Sprintf("gui/%d", currentUserID())
		_ = runServiceCommand("launchctl", "bootout", domain+"/"+clientToolServiceLabel)
		return runServiceCommand("launchctl", "bootstrap", domain, paths.definition)
	case "linux":
		if err := writeServiceDefinition(paths.definition, renderSystemdUserService(executable, home)); err != nil {
			return err
		}
		if err := runServiceCommand("systemctl", "--user", "daemon-reload"); err != nil {
			return err
		}
		return runServiceCommand("systemctl", "--user", "enable", "--now", clientToolLinuxServiceName)
	case "windows":
		command, err := windowsTaskCommand(executable)
		if err != nil {
			return err
		}
		if err := runServiceCommand("schtasks.exe", "/Create", "/F", "/SC", "ONLOGON", "/TN", clientToolWindowsTaskName, "/TR", command); err != nil {
			return err
		}
		return runServiceCommand("schtasks.exe", "/Run", "/TN", clientToolWindowsTaskName)
	default:
		return fmt.Errorf("automatic client-tool service is not supported on %s", runtime.GOOS)
	}
}

func startClientToolService(_, home string) error {
	switch runtime.GOOS {
	case "darwin":
		paths, err := clientToolServiceFilePaths(runtime.GOOS, home)
		if err != nil {
			return err
		}
		domain := fmt.Sprintf("gui/%d", currentUserID())
		if err := runServiceCommand("launchctl", "bootstrap", domain, paths.definition); err == nil {
			return nil
		}
		return runServiceCommand("launchctl", "kickstart", "-k", domain+"/"+clientToolServiceLabel)
	case "linux":
		return runServiceCommand("systemctl", "--user", "start", clientToolLinuxServiceName)
	case "windows":
		return runServiceCommand("schtasks.exe", "/Run", "/TN", clientToolWindowsTaskName)
	default:
		return fmt.Errorf("automatic client-tool service is not supported on %s", runtime.GOOS)
	}
}

func stopClientToolService(_, _ string) error {
	switch runtime.GOOS {
	case "darwin":
		return runServiceCommand("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", currentUserID(), clientToolServiceLabel))
	case "linux":
		return runServiceCommand("systemctl", "--user", "stop", clientToolLinuxServiceName)
	case "windows":
		return runServiceCommand("schtasks.exe", "/End", "/TN", clientToolWindowsTaskName)
	default:
		return fmt.Errorf("automatic client-tool service is not supported on %s", runtime.GOOS)
	}
}

func uninstallClientToolService(_, home string) error {
	paths, err := clientToolServiceFilePaths(runtime.GOOS, home)
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case "darwin":
		domain := fmt.Sprintf("gui/%d", currentUserID())
		_ = runServiceCommand("launchctl", "bootout", domain+"/"+clientToolServiceLabel)
		return removeServiceDefinition(paths.definition)
	case "linux":
		_ = runServiceCommand("systemctl", "--user", "disable", "--now", clientToolLinuxServiceName)
		if err := removeServiceDefinition(paths.definition); err != nil {
			return err
		}
		return runServiceCommand("systemctl", "--user", "daemon-reload")
	case "windows":
		_ = runServiceCommand("schtasks.exe", "/End", "/TN", clientToolWindowsTaskName)
		return runServiceCommand("schtasks.exe", "/Delete", "/F", "/TN", clientToolWindowsTaskName)
	default:
		return fmt.Errorf("automatic client-tool service is not supported on %s", runtime.GOOS)
	}
}

func clientToolServiceFilePaths(goos, home string) (clientToolServicePaths, error) {
	if strings.TrimSpace(home) == "" {
		return clientToolServicePaths{}, errors.New("home directory is required")
	}
	switch goos {
	case "darwin":
		return clientToolServicePaths{
			definition: filepath.Join(home, "Library", "LaunchAgents", clientToolServiceLabel+".plist"),
			log:        filepath.Join(home, ".bitwave", "logs", "client-tools.log"),
		}, nil
	case "linux":
		return clientToolServicePaths{definition: filepath.Join(home, ".config", "systemd", "user", clientToolLinuxServiceName)}, nil
	case "windows":
		return clientToolServicePaths{}, nil
	default:
		return clientToolServicePaths{}, fmt.Errorf("automatic client-tool service is not supported on %s", goos)
	}
}

func writeServiceDefinition(path, contents string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create client-tool service directory: %w", err)
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, []byte(contents), 0o600); err != nil {
		return fmt.Errorf("write client-tool service definition: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("install client-tool service definition: %w", err)
	}
	return nil
}

func removeServiceDefinition(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove client-tool service definition: %w", err)
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
    <string>client-tools</string>
    <string>serve</string>
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
`, xmlText(clientToolServiceLabel), xmlText(executable), xmlText(home), xmlText(logPath), xmlText(logPath))
}

func renderSystemdUserService(executable, home string) string {
	return fmt.Sprintf(`[Unit]
Description=%s

[Service]
Type=simple
ExecStart=%s --quiet client-tools serve --service
WorkingDirectory=%s
Restart=on-failure
RestartSec=10

[Install]
WantedBy=default.target
`, clientToolServiceDescription, systemdQuote(executable), systemdQuote(home))
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
	return fmt.Sprintf(`"%s" --quiet client-tools serve --service`, executable), nil
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

func clientToolBridgeIsRunning() bool {
	connection, err := net.DialTimeout("tcp", defaultClientToolBridgeAddress, 250*time.Millisecond)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}
