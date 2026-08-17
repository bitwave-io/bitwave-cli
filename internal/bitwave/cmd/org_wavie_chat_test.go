package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateBitwaveArgsRejectsRecursiveWavie(t *testing.T) {
	if err := validateBitwaveArgs([]string{"org", "wavie", "chat"}); err == nil {
		t.Fatal("expected recursive Wavie command to be rejected")
	}
	if err := validateBitwaveArgs([]string{"org", "transactions", "list", "--json"}); err != nil {
		t.Fatalf("ordinary args rejected: %v", err)
	}
}

func TestHandleWavieLocalToolApproval(t *testing.T) {
	input, err := json.Marshal(wavieLocalToolInput{Args: []string{"hello world"}, Reason: "test local execution"})
	if err != nil {
		t.Fatal(err)
	}
	var prompt bytes.Buffer
	result := handleWavieLocalTool(
		context.Background(),
		bufio.NewReader(strings.NewReader("yes\n")),
		&prompt,
		"/bin/echo",
		t.TempDir(),
		wavieToolRequestPayload{ToolCallID: "tool-1", Name: wavieLocalToolName, Input: input},
	)
	if result.Status != "ok" || result.ToolCallID != "tool-1" || result.Content != "hello world" {
		t.Fatalf("result = %#v", result)
	}
	if !strings.Contains(prompt.String(), `Command: bitwave "hello world"`) || !strings.Contains(prompt.String(), "Approve once?") {
		t.Fatalf("prompt = %q", prompt.String())
	}
	var data localCommandResult
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.ExitCode != 0 || filepath.Base(data.Command[0]) != "echo" {
		t.Fatalf("command result = %#v", data)
	}
}

func TestHandleWavieLocalToolDenialDoesNotExecute(t *testing.T) {
	input, _ := json.Marshal(wavieLocalToolInput{Args: []string{"should-not-run"}, Reason: "test denial"})
	result := handleWavieLocalTool(
		context.Background(),
		bufio.NewReader(strings.NewReader("no\n")),
		&bytes.Buffer{},
		"/path/that/does/not/exist",
		t.TempDir(),
		wavieToolRequestPayload{ToolCallID: "tool-2", Name: wavieLocalToolName, Input: input},
	)
	if result.Status != "denied" {
		t.Fatalf("result = %#v", result)
	}
}

func TestHandleWavieLocalToolRequiresReason(t *testing.T) {
	input, _ := json.Marshal(wavieLocalToolInput{Args: []string{"--version"}})
	result := handleWavieLocalTool(
		context.Background(),
		bufio.NewReader(strings.NewReader("yes\n")),
		&bytes.Buffer{},
		"/path/that/does/not/exist",
		t.TempDir(),
		wavieToolRequestPayload{ToolCallID: "tool-3", Name: wavieLocalToolName, Input: input},
	)
	if result.Status != "error" || !strings.Contains(result.Content, "non-empty reason") {
		t.Fatalf("result = %#v", result)
	}
}

func TestLimitedBufferCapsOutput(t *testing.T) {
	buffer := &limitedBuffer{limit: 4}
	n, err := buffer.Write([]byte("abcdef"))
	if err != nil || n != 6 || buffer.String() != "abcd" || !buffer.truncated {
		t.Fatalf("n=%d err=%v string=%q truncated=%v", n, err, buffer.String(), buffer.truncated)
	}
}
