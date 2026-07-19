// Package telemetry implements anonymous usage telemetry for the bitwave CLI.
//
// Design constraints, in order:
//   - Nothing sensitive ever leaves the machine: command paths and flag NAMES
//     only — never argument values, payees, amounts, accounts, file paths, or
//     addresses.
//   - Zero latency cost: events append to a local spool; the spool flushes in
//     batches after a command's real work finishes, with a hard timeout.
//     Telemetry can never slow down or fail a command.
//   - Non-interactive first: disclosure is a one-time stderr notice, never a
//     prompt. Opt out with `bitwave telemetry disable`, BITWAVE_TELEMETRY=0,
//     or the cross-tool DO_NOT_TRACK=1 convention.
//
// The wire contract (POST {endpoint} with {"events": [...]}) is documented in
// docs/TELEMETRY.md.
package telemetry

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/bitwave-io/bitwave-cli/internal/apierr"
	"github.com/bitwave-io/bitwave-cli/internal/update"
)

const (
	defaultEndpoint = "https://api.bitwave.io/metrics"
	schemaVersion   = "cli-command/v1"

	flushTimeout   = 2500 * time.Millisecond
	flushBatchMax  = 100 // max events per POST
	flushMinEvents = 10  // flush when the spool has at least this many events…
	flushMaxAge    = time.Hour
	spoolMaxEvents = 200 // …and never hold more than this many (drop oldest)
)

// Event is one CLI invocation. Field names are the wire format.
type Event struct {
	Schema         string   `json:"schema"`
	Ts             string   `json:"ts"`
	AnonymousId    string   `json:"anonymousId"`
	Version        string   `json:"version"`
	Os             string   `json:"os"`
	Arch           string   `json:"arch"`
	InstallChannel string   `json:"installChannel"`
	Command        string   `json:"command"`
	Flags          []string `json:"flags,omitempty"`
	DurationMs     int64    `json:"durationMs"`
	Ok             bool     `json:"ok"`
	ErrorClass     string   `json:"errorClass,omitempty"`
	AgentTokenEnv  bool     `json:"agentTokenEnv"`
	Tty            bool     `json:"tty"`
	Harness        string   `json:"harness,omitempty"`
}

// state is the on-disk config at ~/.bitwave/telemetry.json.
type state struct {
	AnonymousId string `json:"anonymousId"`
	Disabled    bool   `json:"disabled"`
	NoticeShown bool   `json:"noticeShown"`
}

func dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".bitwave"), nil
}

func statePath() (string, error) {
	d, err := dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "telemetry.json"), nil
}

func spoolPath() (string, error) {
	d, err := dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "telemetry-spool.jsonl"), nil
}

func loadState() state {
	var s state
	p, err := statePath()
	if err != nil {
		return s
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return s
	}
	_ = json.Unmarshal(b, &s)
	return s
}

func saveState(s state) {
	p, err := statePath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	b, _ := json.Marshal(s)
	_ = os.WriteFile(p, b, 0o644)
}

// Decision explains whether telemetry is on and why — surfaced by
// `bitwave telemetry status`.
type Decision struct {
	Enabled bool
	Reason  string // "BITWAVE_TELEMETRY env", "DO_NOT_TRACK env", "disabled via `bitwave telemetry disable`", "default (enabled)", "dev build"
}

// Decide resolves the opt-out precedence: explicit env > dev-build guard >
// DO_NOT_TRACK > persisted choice > default-on. Dev/snapshot builds never
// send by default, but an explicit BITWAVE_TELEMETRY=1 overrides (useful to
// exercise the pipeline before a release).
func Decide(version string) Decision {
	switch os.Getenv("BITWAVE_TELEMETRY") {
	case "0", "false", "off":
		return Decision{false, "BITWAVE_TELEMETRY env"}
	case "1", "true", "on":
		return Decision{true, "BITWAVE_TELEMETRY env"}
	}
	if strings.Contains(version, "-") {
		return Decision{false, "dev build"}
	}
	if v := os.Getenv("DO_NOT_TRACK"); v != "" && v != "0" {
		return Decision{false, "DO_NOT_TRACK env"}
	}
	if loadState().Disabled {
		return Decision{false, "disabled via `bitwave telemetry disable`"}
	}
	return Decision{true, "default (enabled)"}
}

// Endpoint returns the ingest URL (override: BITWAVE_TELEMETRY_URL).
func Endpoint() string {
	if v := os.Getenv("BITWAVE_TELEMETRY_URL"); v != "" {
		return v
	}
	return defaultEndpoint
}

// NoticeIfNeeded prints the one-time disclosure to stderr and marks it shown.
// Never a prompt: this is disclosure for humans and agent transcripts alike.
// Suppressed (and left pending) under quiet mode so it surfaces on a later
// non-quiet run.
func NoticeIfNeeded(version string, quiet bool) {
	if quiet || !Decide(version).Enabled {
		return
	}
	s := loadState()
	if s.NoticeShown {
		return
	}
	fmt.Fprintln(os.Stderr, "bitwave: anonymous usage telemetry is on (command names + flag names only — never values, ledger data, or file paths).")
	fmt.Fprintln(os.Stderr, "bitwave: disable anytime: `bitwave telemetry disable` or BITWAVE_TELEMETRY=0. Details: https://github.com/bitwave-io/bitwave-cli/blob/main/docs/TELEMETRY.md")
	s.NoticeShown = true
	saveState(s)
}

// RecordCommand appends one event to the spool. Safe to call with a nil
// command (nothing recorded). All errors are swallowed.
func RecordCommand(version string, c *cobra.Command, runErr error, duration time.Duration) {
	if c == nil || !Decide(version).Enabled {
		return
	}
	cmdPath := strings.TrimSpace(strings.TrimPrefix(c.CommandPath(), "bitwave"))
	if cmdPath == "" {
		cmdPath = "(root)"
	}
	// The telemetry command itself is noise.
	if strings.HasPrefix(cmdPath, "telemetry") {
		return
	}

	var flags []string
	seen := map[string]bool{}
	collect := func(f *pflag.Flag) {
		if !seen[f.Name] {
			seen[f.Name] = true
			flags = append(flags, f.Name)
		}
	}
	c.Flags().Visit(collect)
	c.InheritedFlags().Visit(collect)

	exe, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	ev := Event{
		Schema:         schemaVersion,
		Ts:             time.Now().UTC().Format(time.RFC3339),
		AnonymousId:    anonymousId(),
		Version:        strings.TrimPrefix(version, "v"),
		Os:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		InstallChannel: string(update.DetectInstallMethod(exe)),
		Command:        cmdPath,
		Flags:          flags,
		DurationMs:     duration.Milliseconds(),
		Ok:             runErr == nil,
		ErrorClass:     classifyError(runErr),
		AgentTokenEnv:  os.Getenv("BITWAVE_AGENT_TOKEN") != "",
		Tty:            stderrIsTTY(),
		Harness:        detectHarness(),
	}
	appendSpool(ev)
}

// anonymousId returns (creating if needed) a random machine-local id. It is
// never derived from hardware, usernames, or network identity.
func anonymousId() string {
	s := loadState()
	if s.AnonymousId != "" {
		return s.AnonymousId
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "unknown"
	}
	s.AnonymousId = hex.EncodeToString(b)
	saveState(s)
	return s.AnonymousId
}

func classifyError(err error) string {
	if err == nil {
		return ""
	}
	var apiErr *apierr.Error
	if errors.As(err, &apiErr) {
		if apiErr.Status >= 500 {
			return "api_5xx"
		}
		return "api_4xx"
	}
	msg := err.Error()
	if strings.Contains(msg, "unknown command") || strings.Contains(msg, "unknown flag") ||
		strings.Contains(msg, "required") || strings.Contains(msg, "accepts") {
		return "usage"
	}
	return "other"
}

func stderrIsTTY() bool {
	fi, err := os.Stderr.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// detectHarness identifies well-known agent harnesses from ambient env.
// Presence only — no env values are collected.
func detectHarness() string {
	switch {
	case os.Getenv("CLAUDECODE") != "":
		return "claude-code"
	case os.Getenv("CURSOR_AGENT") != "":
		return "cursor"
	case os.Getenv("GEMINI_CLI") != "":
		return "gemini-cli"
	case os.Getenv("CODEX_SANDBOX") != "":
		return "codex"
	default:
		return ""
	}
}

func appendSpool(ev Event) {
	p, err := spoolPath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	line, err := json.Marshal(ev)
	if err != nil {
		return
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_, _ = f.Write(append(line, '\n'))
	_ = f.Close()
	capSpool(p)
}

// capSpool keeps the spool bounded when the endpoint is unreachable for a
// long time: oldest events are dropped beyond spoolMaxEvents.
func capSpool(p string) {
	events := readSpool(p)
	if len(events) <= spoolMaxEvents {
		return
	}
	writeSpool(p, events[len(events)-spoolMaxEvents:])
}

func readSpool(p string) []json.RawMessage {
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []json.RawMessage
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		out = append(out, json.RawMessage(append([]byte(nil), line...)))
	}
	return out
}

func writeSpool(p string, events []json.RawMessage) {
	var buf bytes.Buffer
	for _, e := range events {
		buf.Write(e)
		buf.WriteByte('\n')
	}
	_ = os.WriteFile(p, buf.Bytes(), 0o644)
}

// MaybeFlush sends the spool to the ingest endpoint when it is due (enough
// events, or the oldest is stale). Runs after a command's work is complete;
// silent on every failure. Set force to flush regardless of thresholds.
func MaybeFlush(version string, force bool) {
	if !Decide(version).Enabled {
		return
	}
	p, err := spoolPath()
	if err != nil {
		return
	}
	events := readSpool(p)
	if len(events) == 0 {
		return
	}
	if !force && len(events) < flushMinEvents && !spoolStale(p) {
		return
	}
	batch := events
	if len(batch) > flushBatchMax {
		batch = batch[:flushBatchMax]
	}
	body, err := json.Marshal(map[string]any{"events": batch})
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, Endpoint(), bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		writeSpool(p, events[len(batch):])
	}
}

func spoolStale(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && time.Since(fi.ModTime()) > flushMaxAge
}

// SpoolCount reports how many events are waiting locally.
func SpoolCount() int {
	p, err := spoolPath()
	if err != nil {
		return 0
	}
	return len(readSpool(p))
}

// SetDisabled persists the user's choice; disabling also wipes the spool so
// nothing recorded earlier can be sent later.
func SetDisabled(disabled bool) {
	s := loadState()
	s.Disabled = disabled
	saveState(s)
	if disabled {
		if p, err := spoolPath(); err == nil {
			_ = os.Remove(p)
		}
	}
}

// AnonymousIdForStatus exposes the id for `telemetry status` without
// creating one as a side effect.
func AnonymousIdForStatus() string {
	return loadState().AnonymousId
}
