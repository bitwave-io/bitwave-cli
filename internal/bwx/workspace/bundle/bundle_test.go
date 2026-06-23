package bundle

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile is a tiny helper for setting up fixture workspaces.
func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// readZip returns a name→content map for assertions.
func readZip(t *testing.T, raw []byte) map[string]string {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("zip open: %v", err)
	}
	out := map[string]string{}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		data, _ := io.ReadAll(rc)
		_ = rc.Close()
		out[f.Name] = string(data)
	}
	return out
}

func TestBuild_IncludesAllowedFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".bwx.toml", "mode = \"local\"\nbase_currency = \"USD\"\n")
	writeFile(t, dir, "accounts.ledger", "account Assets:Cash\n")
	writeFile(t, dir, "prices.ledger", "P 2024-01-15 BTC $50000.00\n")
	writeFile(t, dir, "default.journal", "2024-01-15 * Coffee\n    Expenses:Food  $5\n    Assets:Cash   -$5\n")

	var buf bytes.Buffer
	res, err := Build(dir, "default", &buf)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	files := readZip(t, buf.Bytes())
	for _, want := range []string{".bwx.toml", "accounts.ledger", "prices.ledger", "default.journal"} {
		if _, ok := files[want]; !ok {
			t.Errorf("missing %s in bundle (have %v)", want, keys(files))
		}
	}
	if res.IncludedFiles != 4 {
		t.Errorf("IncludedFiles = %d, want 4", res.IncludedFiles)
	}
	if len(res.ExcludedFiles) != 0 {
		t.Errorf("ExcludedFiles should be empty, got %v", res.ExcludedFiles)
	}
}

func TestBuild_RejectsMissingJournal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".bwx.toml", "mode = \"local\"\n")
	writeFile(t, dir, "accounts.ledger", "")

	var buf bytes.Buffer
	_, err := Build(dir, "missing", &buf)
	if !errors.Is(err, ErrJournalNotFound) {
		t.Fatalf("expected ErrJournalNotFound, got %v", err)
	}
}

func TestBuild_ExcludesOtherJournals(t *testing.T) {
	// Only the target journal goes into the bundle. The recipient should not
	// receive unrelated journals from the workspace.
	dir := t.TempDir()
	writeFile(t, dir, ".bwx.toml", "")
	writeFile(t, dir, "default.journal", "; default\n")
	writeFile(t, dir, "secret-payroll.journal", "; do not share\n")

	var buf bytes.Buffer
	_, err := Build(dir, "default", &buf)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	files := readZip(t, buf.Bytes())
	if _, ok := files["secret-payroll.journal"]; ok {
		t.Fatal("non-target journal leaked into bundle")
	}
	if _, ok := files["default.journal"]; !ok {
		t.Fatal("target journal missing from bundle")
	}
}

// All denylist patterns must fail-closed: the file is refused (Build returns
// an error) rather than silently skipped. A silent skip would let a misnamed
// secret slip into the zip; refusing forces the user to remove or rename it.
func TestBuild_FailsClosedOnDenyPatterns(t *testing.T) {
	cases := []struct {
		name string
		rel  string
		body string
	}{
		{"private-key pem", "ec-key.pem", "-----BEGIN EC PRIVATE KEY-----"},
		{"raw .key", "wallet.key", "secret"},
		{"keys directory", "keys/eth.key", "secret"},
		{"wallets directory keystore", "wallets/cold.json", `{"crypto":{}}`},
		{"top-level wallet-*.json", "wallet-acme.json", `{"crypto":{}}`},
		{"dotenv", ".env", "API_KEY=abc"},
		{"dotenv variant", ".env.local", "X=1"},
		{"mnemonic txt", "mnemonic.txt", "twelve word phrase here"},
		{"seed file", "seed.txt", "..."},
		{"credentials json", "credentials.json", `{"token":"x"}`},
		{"id_rsa", "id_rsa", "..."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, ".bwx.toml", "")
			writeFile(t, dir, "default.journal", "")
			writeFile(t, dir, tc.rel, tc.body)

			var buf bytes.Buffer
			_, err := Build(dir, "default", &buf)
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.rel)
			}
			var se *SensitiveFileError
			if !errors.As(err, &se) {
				t.Fatalf("expected SensitiveFileError, got %T: %v", err, err)
			}
			if se.Path != tc.rel {
				t.Errorf("SensitiveFileError.Path = %q, want %q", se.Path, tc.rel)
			}
		})
	}
}

func TestBuild_FailsClosedOnUnknownBinary(t *testing.T) {
	// An unknown file with non-text content is refused even if it doesn't
	// match a known sensitive pattern — the operator must explicitly approve
	// or remove it before sharing.
	dir := t.TempDir()
	writeFile(t, dir, ".bwx.toml", "")
	writeFile(t, dir, "default.journal", "")
	bin := filepath.Join(dir, "mystery.bin")
	if err := os.WriteFile(bin, []byte{0x00, 0x01, 0x02, 0xff, 0xfe}, 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	_, err := Build(dir, "default", &buf)
	if !errors.Is(err, ErrUnknownBinaryFile) {
		t.Fatalf("expected ErrUnknownBinaryFile, got %v", err)
	}
}

func TestBuild_SkipsHiddenDirs(t *testing.T) {
	// .git and other dot-directories are never bundled even when they
	// contain only text — they're not workspace data.
	dir := t.TempDir()
	writeFile(t, dir, ".bwx.toml", "")
	writeFile(t, dir, "default.journal", "")
	writeFile(t, dir, ".git/HEAD", "ref: refs/heads/main\n")
	writeFile(t, dir, ".idea/workspace.xml", "<x/>")

	var buf bytes.Buffer
	res, err := Build(dir, "default", &buf)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	files := readZip(t, buf.Bytes())
	for name := range files {
		if strings.HasPrefix(name, ".git/") || strings.HasPrefix(name, ".idea/") {
			t.Errorf("hidden dir leaked: %s", name)
		}
	}
	for _, p := range res.SkippedDirs {
		if p != ".git" && p != ".idea" {
			t.Errorf("unexpected skipped dir: %s", p)
		}
	}
}

func TestBuild_IncludesOtherLedgerFiles(t *testing.T) {
	// Ad-hoc .ledger files (e.g. opening-balances.ledger) ARE workspace data
	// and should be included. Only target .journal is special.
	dir := t.TempDir()
	writeFile(t, dir, ".bwx.toml", "")
	writeFile(t, dir, "default.journal", "")
	writeFile(t, dir, "opening-balances.ledger", "; OB\n")

	var buf bytes.Buffer
	_, err := Build(dir, "default", &buf)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	files := readZip(t, buf.Bytes())
	if _, ok := files["opening-balances.ledger"]; !ok {
		t.Errorf("opening-balances.ledger should be included")
	}
}

func TestBuild_SizeCapEnforced(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".bwx.toml", "")
	writeFile(t, dir, "default.journal", "")
	// Big text file (still text, so doesn't trip ErrUnknownBinaryFile)
	big := strings.Repeat("a\n", maxFileSizeBytes+1)
	writeFile(t, dir, "huge.ledger", big)

	var buf bytes.Buffer
	_, err := Build(dir, "default", &buf)
	if !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("expected ErrFileTooLarge, got %v", err)
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
