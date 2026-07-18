// Package bundle builds a sanitized .zip of a local wavie workspace for
// sharing. It is fail-closed: any file matching a sensitive-name pattern, or
// any unknown binary file, aborts the bundle. Operators must remove or rename
// the file before retrying. Silently dropping such files would risk leaking a
// misnamed secret into a recipient's inbox.
package bundle

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	// maxFileSizeBytes caps any individual file. Workspace ledger files are
	// human-edited text; runaway sizes indicate a non-workspace file.
	maxFileSizeBytes = 5 * 1024 * 1024 // 5 MiB
	// maxBundleSizeBytes caps the total bundle. Hard upper bound so a
	// pathological workspace can't OOM the CLI or the server.
	maxBundleSizeBytes = 25 * 1024 * 1024 // 25 MiB
)

var (
	ErrJournalNotFound   = errors.New("target journal file not found in workspace")
	ErrUnknownBinaryFile = errors.New("unknown binary file in workspace (remove or rename)")
	ErrFileTooLarge      = errors.New("file exceeds bundle size cap")
	ErrBundleTooLarge    = errors.New("bundle would exceed total size cap")
)

// SensitiveFileError wraps a denylist hit with the offending path.
type SensitiveFileError struct {
	Path   string
	Reason string
}

func (e *SensitiveFileError) Error() string {
	return fmt.Sprintf("sensitive file refused: %s (%s)", e.Path, e.Reason)
}

// Result reports what was included or skipped. ExcludedFiles is empty when
// Build succeeds — Build refuses to ship if any file would have been excluded
// for sensitivity. SkippedDirs lists dot-directories that were never walked.
type Result struct {
	IncludedFiles int
	ExcludedFiles []string
	SkippedDirs   []string
	Bytes         int64
}

// Build writes a sanitized zip of the workspace at dir into w. The target
// journal file (<journalId>.journal) must exist. Other .journal files are
// excluded — the caller's intent is to share one journal, not all of them.
func Build(dir, journalId string, w io.Writer) (*Result, error) {
	if _, err := os.Stat(filepath.Join(dir, journalId+".journal")); err != nil {
		if os.IsNotExist(err) {
			return nil, ErrJournalNotFound
		}
		return nil, err
	}

	zw := zip.NewWriter(w)
	defer func() { _ = zw.Close() }()

	res := &Result{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			if isHiddenDir(d.Name()) {
				res.SkippedDirs = append(res.SkippedDirs, rel)
				return fs.SkipDir
			}
			return nil
		}

		// Non-target journals are not part of this share.
		if isOtherJournal(rel, journalId) {
			return nil
		}

		if reason, sensitive := sensitiveReason(rel); sensitive {
			return &SensitiveFileError{Path: rel, Reason: reason}
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxFileSizeBytes {
			return fmt.Errorf("%w: %s", ErrFileTooLarge, rel)
		}
		res.Bytes += info.Size()
		if res.Bytes > maxBundleSizeBytes {
			return fmt.Errorf("%w: bundle exceeds %d bytes", ErrBundleTooLarge, maxBundleSizeBytes)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		// Allowlist by name first; if not on the allowlist, the file must look
		// like text. This catches accidental binary blobs that don't trip a
		// known sensitive pattern.
		if !isAllowedName(rel) {
			if !looksLikeText(data) {
				return fmt.Errorf("%w: %s", ErrUnknownBinaryFile, rel)
			}
		}

		fw, err := zw.Create(rel)
		if err != nil {
			return err
		}
		if _, err := fw.Write(data); err != nil {
			return err
		}
		res.IncludedFiles++
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

func isHiddenDir(name string) bool {
	return strings.HasPrefix(name, ".") && name != "." && name != ".."
}

func isOtherJournal(rel, journalId string) bool {
	if !strings.HasSuffix(rel, ".journal") {
		return false
	}
	if strings.Contains(rel, "/") {
		// Nested .journal files aren't part of the canonical layout; skip.
		return true
	}
	return rel != journalId+".journal"
}

// isAllowedName lists filenames that are unambiguously workspace data. Files
// that don't match still pass if they look like text (caught by looksLikeText
// instead).
func isAllowedName(rel string) bool {
	if strings.Contains(rel, "/") {
		return false
	}
	switch rel {
	case ".wavie.toml", "accounts.ledger", "prices.ledger":
		return true
	}
	if strings.HasSuffix(rel, ".journal") {
		return true
	}
	if strings.HasSuffix(rel, ".ledger") {
		return true
	}
	return false
}

// sensitiveReason returns a non-empty reason when rel matches a known
// secret/credential pattern. Patterns are matched against the path; both
// top-level and nested matches count.
func sensitiveReason(rel string) (string, bool) {
	lower := strings.ToLower(rel)
	base := filepath.Base(lower)

	if strings.HasPrefix(lower, "keys/") || strings.Contains(lower, "/keys/") || lower == "keys" {
		return "keys directory", true
	}
	if strings.HasPrefix(lower, "wallets/") || strings.Contains(lower, "/wallets/") {
		return "wallets directory", true
	}
	if strings.HasPrefix(base, "wallet-") && strings.HasSuffix(base, ".json") {
		return "wallet keystore", true
	}
	if strings.HasSuffix(base, ".key") || strings.HasSuffix(base, ".pem") {
		return "private key file", true
	}
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return "env file (may contain secrets)", true
	}
	if base == "mnemonic.txt" || base == "seed.txt" || base == "mnemonic" || base == "seed" {
		return "mnemonic/seed phrase", true
	}
	if base == "credentials.json" || base == "credentials" {
		return "credentials file", true
	}
	if base == "id_rsa" || base == "id_dsa" || base == "id_ecdsa" || base == "id_ed25519" {
		return "ssh private key", true
	}
	return "", false
}

// looksLikeText reports whether the buffer is plausibly UTF-8 text. NUL bytes
// or a high ratio of non-printable bytes flips this to false.
func looksLikeText(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	sample := data
	if len(sample) > 8192 {
		sample = sample[:8192]
	}
	nonPrintable := 0
	for _, b := range sample {
		if b == 0 {
			return false
		}
		if b < 0x09 || (b > 0x0d && b < 0x20 && b != 0x1b) {
			nonPrintable++
		}
	}
	return nonPrintable*32 < len(sample) // < ~3% non-printable
}
