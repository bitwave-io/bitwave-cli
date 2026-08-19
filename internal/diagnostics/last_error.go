// Package diagnostics persists a small, redacted record of the most recent
// failed CLI invocation so it can be attached to a support request.
package diagnostics

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/bitwave-io/bitwave-cli/internal/apierr"
)

var ErrNoRecordedError = errors.New("no CLI error has been recorded")

type LastError struct {
	SchemaVersion string    `json:"schemaVersion"`
	RecordedAt    time.Time `json:"recordedAt"`
	Command       string    `json:"command,omitempty"`
	Message       string    `json:"message"`
	HTTPStatus    int       `json:"httpStatus,omitempty"`
	HTTPMethod    string    `json:"httpMethod,omitempty"`
	RequestURL    string    `json:"requestUrl,omitempty"`
	Detail        string    `json:"detail,omitempty"`
}

func Record(command string, invocationErr error) error {
	if invocationErr == nil {
		return nil
	}
	record := LastError{
		SchemaVersion: "1",
		RecordedAt:    time.Now().UTC(),
		Command:       strings.TrimSpace(command),
		Message:       redact(invocationErr.Error()),
	}
	var apiError *apierr.Error
	if errors.As(invocationErr, &apiError) {
		record.HTTPStatus = apiError.Status
		record.HTTPMethod = apiError.Method
		record.RequestURL = safeURL(apiError.URL)
		record.Detail = redact(apiError.Detail)
		record.Message = fmt.Sprintf("%s request failed with HTTP %d", apiError.Method, apiError.Status)
		if record.Detail != "" {
			record.Message += ": " + record.Detail
		}
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	path, err := statePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}

func Load() (*LastError, error) {
	path, err := statePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoRecordedError
		}
		return nil, fmt.Errorf("read last CLI error: %w", err)
	}
	var record LastError
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("decode last CLI error: %w", err)
	}
	return &record, nil
}

func statePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".bitwave", "last-error.json"), nil
}

func safeURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.User = nil
	return parsed.String()
}

var secretPattern = regexp.MustCompile(`(?i)(bearer\s+|(?:access[_-]?)?token["']?\s*[=:]\s*["']?|(?:client[_-]?)?(?:secret|key)["']?\s*[=:]\s*["']?)[^"'\s&,}]+`)

func redact(value string) string {
	value = secretPattern.ReplaceAllString(value, "${1}[REDACTED]")
	const max = 2000
	if len(value) > max {
		return value[:max] + "…"
	}
	return value
}
