package app

import (
	"bytes"
	"reflect"
	"testing"
)

func TestRunPrintsBuildVersionWithoutRequiringAFile(t *testing.T) {
	originalVersion := BuildVersion
	BuildVersion = "continuous+0123456789abcdef"
	t.Cleanup(func() {
		BuildVersion = originalVersion
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := Run([]string{"--version"}, nil, &stdout, &stderr); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := stdout.String(), "docxedit continuous+0123456789abcdef\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want empty output", got)
	}
}

func TestSplitCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    []string
	}{
		{name: "plain", command: "code --wait", want: []string{"code", "--wait"}},
		{name: "quoted app", command: `open -a "Visual Studio Code"`, want: []string{"open", "-a", "Visual Studio Code"}},
		{name: "escaped space", command: `my\ editor --flag`, want: []string{"my editor", "--flag"}},
		{name: "windows path", command: `"C:\Program Files\Editor\edit.exe" --wait`, want: []string{`C:\Program Files\Editor\edit.exe`, "--wait"}},
		{name: "empty argument", command: `editor ""`, want: []string{"editor", ""}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := splitCommand(test.command)
			if err != nil {
				t.Fatalf("splitCommand() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("splitCommand() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestSplitCommandRejectsUnterminatedQuote(t *testing.T) {
	if _, err := splitCommand(`editor "unfinished`); err == nil {
		t.Fatal("splitCommand() accepted an unterminated quote")
	}
}
