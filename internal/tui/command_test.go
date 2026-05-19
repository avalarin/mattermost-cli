package tui

import (
	"errors"
	"strings"
	"testing"
)

// buildTestRegistry creates a registry with /quit, /send, and /help for testing.
func buildTestRegistry() *Registry {
	return buildRegistry(nil, "team1", func() {})
}

func TestParseValidSendToChannel(t *testing.T) {
	r := buildTestRegistry()

	result, err := r.Parse("/send #general Привет мир")
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	if result.Def.Name != "send" {
		t.Errorf("expected command 'send', got %q", result.Def.Name)
	}
	if got := result.Args["target"]; got != "#general" {
		t.Errorf("target = %q, want %q", got, "#general")
	}
	if got := result.Args["text"]; got != "Привет мир" {
		t.Errorf("text = %q, want %q", got, "Привет мир")
	}
}

func TestParseValidSendDM(t *testing.T) {
	r := buildTestRegistry()

	result, err := r.Parse("/send @alice Привет")
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	if got := result.Args["target"]; got != "@alice" {
		t.Errorf("target = %q, want %q", got, "@alice")
	}
	if got := result.Args["text"]; got != "Привет" {
		t.Errorf("text = %q, want %q", got, "Привет")
	}
}

func TestParseInvalidSendNoArgs(t *testing.T) {
	r := buildTestRegistry()

	_, err := r.Parse("/send")
	if err == nil {
		t.Fatal("expected error for /send with no args, got nil")
	}
	if !errors.Is(err, ErrMissingArg) {
		t.Errorf("expected ErrMissingArg, got: %v", err)
	}
}

func TestParseUnknownCommand(t *testing.T) {
	r := buildTestRegistry()

	_, err := r.Parse("/foo bar")
	if err == nil {
		t.Fatal("expected error for unknown command, got nil")
	}
	if !errors.Is(err, ErrUnknownCommand) {
		t.Errorf("expected ErrUnknownCommand, got: %v", err)
	}
}

func TestRegistryHelpText(t *testing.T) {
	r := buildTestRegistry()

	help := r.HelpText("")
	if help == "" {
		t.Fatal("HelpText(\"\") returned empty string")
	}
	// Should list all three commands.
	for _, name := range []string{"quit", "send", "help"} {
		if !strings.Contains(help, name) {
			t.Errorf("HelpText(\"\") missing command %q", name)
		}
	}
}

func TestRegistryHelpTextSpecific(t *testing.T) {
	r := buildTestRegistry()

	help := r.HelpText("send")
	if help == "" {
		t.Fatal("HelpText(\"send\") returned empty string")
	}
	if !strings.Contains(help, "send") {
		t.Errorf("HelpText(\"send\") missing command name, got: %q", help)
	}
	if !strings.Contains(help, "target") {
		t.Errorf("HelpText(\"send\") missing 'target' argument, got: %q", help)
	}
	if !strings.Contains(help, "text") {
		t.Errorf("HelpText(\"send\") missing 'text' argument, got: %q", help)
	}
}

func TestCommandDefUsage(t *testing.T) {
	def := &CommandDef{
		Name: "send",
		Args: []ArgSpec{
			{Name: "target", Required: true},
			{Name: "text", Required: true, Greedy: true},
		},
	}
	got := def.Usage()
	want := "/send <target> <text>"
	if got != want {
		t.Errorf("Usage() = %q, want %q", got, want)
	}
}

func TestCommandDefUsageOptional(t *testing.T) {
	def := &CommandDef{
		Name: "help",
		Args: []ArgSpec{
			{Name: "command", Required: false},
		},
	}
	got := def.Usage()
	want := "/help [command]"
	if got != want {
		t.Errorf("Usage() = %q, want %q", got, want)
	}
}

func TestParseInvalidSyntax(t *testing.T) {
	r := buildTestRegistry()

	_, err := r.Parse("not a command")
	if !errors.Is(err, ErrInvalidSyntax) {
		t.Errorf("expected ErrInvalidSyntax for non-/ input, got: %v", err)
	}
}

func TestParseGreedyJoinsTokens(t *testing.T) {
	r := buildTestRegistry()

	result, err := r.Parse("/send #chan hello world foo")
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	if got := result.Args["text"]; got != "hello world foo" {
		t.Errorf("greedy text = %q, want %q", got, "hello world foo")
	}
}
