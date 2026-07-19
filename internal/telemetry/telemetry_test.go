package telemetry

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// useTempHome points state + spool at a scratch dir.
func useTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("BITWAVE_TELEMETRY", "")
	t.Setenv("DO_NOT_TRACK", "")
	return home
}

func TestDecide_Precedence(t *testing.T) {
	useTempHome(t)

	if d := Decide("0.2.3"); !d.Enabled {
		t.Errorf("default should be enabled, got %+v", d)
	}
	if d := Decide("0.1.0-dev"); d.Enabled || d.Reason != "dev build" {
		t.Errorf("dev build should be disabled, got %+v", d)
	}

	t.Setenv("DO_NOT_TRACK", "1")
	if d := Decide("0.2.3"); d.Enabled {
		t.Errorf("DO_NOT_TRACK should disable, got %+v", d)
	}
	t.Setenv("BITWAVE_TELEMETRY", "1")
	if d := Decide("0.2.3"); !d.Enabled {
		t.Errorf("explicit BITWAVE_TELEMETRY=1 should win over DO_NOT_TRACK, got %+v", d)
	}
	t.Setenv("BITWAVE_TELEMETRY", "0")
	if d := Decide("0.2.3"); d.Enabled {
		t.Errorf("BITWAVE_TELEMETRY=0 should disable, got %+v", d)
	}

	t.Setenv("BITWAVE_TELEMETRY", "")
	t.Setenv("DO_NOT_TRACK", "")
	SetDisabled(true)
	if d := Decide("0.2.3"); d.Enabled {
		t.Errorf("persisted disable should hold, got %+v", d)
	}
	SetDisabled(false)
	if d := Decide("0.2.3"); !d.Enabled {
		t.Errorf("re-enable should hold, got %+v", d)
	}
}

func TestRecordCommand_FlagNamesOnly(t *testing.T) {
	useTempHome(t)

	c := &cobra.Command{Use: "new"}
	parent := &cobra.Command{Use: "je"}
	rootC := &cobra.Command{Use: "bitwave"}
	rootC.AddCommand(parent)
	parent.AddCommand(c)
	c.Flags().String("payee", "", "")
	c.Flags().String("date", "", "")
	_ = c.Flags().Set("payee", "SUPER SECRET VENDOR")

	RecordCommand("0.2.3", c, nil, 12*time.Millisecond)

	if SpoolCount() != 1 {
		t.Fatalf("spool count = %d, want 1", SpoolCount())
	}
	p, _ := spoolPath()
	raw, _ := os.ReadFile(p)
	var ev Event
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Command != "je new" {
		t.Errorf("command = %q, want %q", ev.Command, "je new")
	}
	if len(ev.Flags) != 1 || ev.Flags[0] != "payee" {
		t.Errorf("flags = %v, want [payee]", ev.Flags)
	}
	if strings.Contains(string(raw), "SUPER SECRET VENDOR") {
		t.Errorf("flag VALUE leaked into event: %s", raw)
	}
	if ev.AnonymousId == "" || ev.AnonymousId == "unknown" {
		t.Errorf("anonymousId missing: %q", ev.AnonymousId)
	}
}

func TestSpoolCapAndDisableWipes(t *testing.T) {
	useTempHome(t)

	c := &cobra.Command{Use: "bal"}
	rootC := &cobra.Command{Use: "bitwave"}
	rootC.AddCommand(c)
	for i := 0; i < spoolMaxEvents+25; i++ {
		RecordCommand("0.2.3", c, nil, time.Millisecond)
	}
	if got := SpoolCount(); got != spoolMaxEvents {
		t.Errorf("spool count = %d, want cap %d", got, spoolMaxEvents)
	}
	SetDisabled(true)
	if got := SpoolCount(); got != 0 {
		t.Errorf("disable should wipe spool, got %d", got)
	}
}

func TestMaybeFlush_SendsBatchAndClearsSpool(t *testing.T) {
	useTempHome(t)

	var gotBatch struct {
		Events []Event `json:"events"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBatch)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	t.Setenv("BITWAVE_TELEMETRY_URL", srv.URL)

	c := &cobra.Command{Use: "bal"}
	rootC := &cobra.Command{Use: "bitwave"}
	rootC.AddCommand(c)
	for i := 0; i < 3; i++ {
		RecordCommand("0.2.3", c, nil, time.Millisecond)
	}

	MaybeFlush("0.2.3", false) // below flushMinEvents, not stale -> no send
	if len(gotBatch.Events) != 0 {
		t.Fatalf("flush fired below threshold")
	}
	MaybeFlush("0.2.3", true) // forced
	if len(gotBatch.Events) != 3 {
		t.Fatalf("forced flush sent %d events, want 3", len(gotBatch.Events))
	}
	if SpoolCount() != 0 {
		t.Errorf("spool not cleared after accepted flush: %d", SpoolCount())
	}
}

func TestNotice_OnceAndQuietDefers(t *testing.T) {
	home := useTempHome(t)

	NoticeIfNeeded("0.2.3", true) // quiet: suppressed AND not marked shown
	if loadState().NoticeShown {
		t.Fatal("quiet run must not mark the notice as shown")
	}
	NoticeIfNeeded("0.2.3", false)
	if !loadState().NoticeShown {
		t.Fatal("non-quiet run should mark the notice shown")
	}
	// State landed in the temp home, not the real one.
	if _, err := os.Stat(filepath.Join(home, ".bitwave", "telemetry.json")); err != nil {
		t.Fatalf("state not in temp home: %v", err)
	}
}
