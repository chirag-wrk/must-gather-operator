package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestObfuscateCmd_helpListsRequiredFlags(t *testing.T) {
	cmd := newObfuscateCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	if err := cmd.Help(); err != nil {
		t.Fatalf("Help: %v", err)
	}
	helpText := buf.String()
	for _, flag := range []string{"--input", "--output", "--config"} {
		if !strings.Contains(helpText, flag) {
			t.Fatalf("help text missing %q:\n%s", flag, helpText)
		}
	}
}

func TestObfuscateCmd_requiresInputAndOutput(t *testing.T) {
	cmd := newObfuscateCmd()
	cmd.SetArgs([]string{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when required flags missing")
	}
}
