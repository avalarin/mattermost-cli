package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

var (
	// ErrUnknownCommand is returned when the command name is not registered.
	ErrUnknownCommand = errors.New("unknown command")
	// ErrMissingArg is returned when a required argument is absent.
	ErrMissingArg = errors.New("missing argument")
	// ErrInvalidSyntax is returned when the input does not start with "/".
	ErrInvalidSyntax = errors.New("invalid syntax")
)

// ArgSpec defines a single argument for a command.
type ArgSpec struct {
	Name        string
	Description string
	Required    bool
	// Greedy, when true, consumes all remaining tokens (must be the last arg).
	Greedy bool
}

// CommandDef is a registered command.
type CommandDef struct {
	Name        string
	Description string
	Args        []ArgSpec
	// Execute runs the command. Return tea.Quit for quit, async tea.Cmd for network ops.
	Execute func(args map[string]string) tea.Cmd
}

// Usage returns a human-readable usage string, e.g. "/send <target> <text>".
func (c *CommandDef) Usage() string {
	parts := []string{"/" + c.Name}
	for _, a := range c.Args {
		if a.Required {
			parts = append(parts, "<"+a.Name+">")
		} else {
			parts = append(parts, "["+a.Name+"]")
		}
	}
	return strings.Join(parts, " ")
}

// ParseResult is the output of a successful Registry.Parse call.
type ParseResult struct {
	Def  *CommandDef
	Args map[string]string
}

// Registry holds all registered commands.
type Registry struct {
	cmds  map[string]*CommandDef
	order []string // deterministic iteration order for /help
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		cmds: make(map[string]*CommandDef),
	}
}

// Register adds a command to the registry. Panics on duplicate name.
func (r *Registry) Register(cmd *CommandDef) {
	if _, exists := r.cmds[cmd.Name]; exists {
		panic("command already registered: " + cmd.Name)
	}
	r.cmds[cmd.Name] = cmd
	r.order = append(r.order, cmd.Name)
}

// Parse parses a command string like "/send #general Hello world".
// Matches positional args in ArgSpec order. A Greedy arg takes all remaining tokens joined by space.
// Returns ErrInvalidSyntax if input doesn't start with "/",
// ErrUnknownCommand if the command is not registered,
// or ErrMissingArg if a required argument is missing.
func (r *Registry) Parse(input string) (*ParseResult, error) {
	text := strings.TrimSpace(input)
	if !strings.HasPrefix(text, "/") {
		return nil, ErrInvalidSyntax
	}
	parts := strings.Fields(text)
	name := strings.TrimPrefix(parts[0], "/")
	def, ok := r.cmds[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownCommand, name)
	}
	rest := parts[1:]
	args := make(map[string]string, len(def.Args))
	for i, spec := range def.Args {
		if spec.Greedy {
			if i < len(rest) {
				args[spec.Name] = strings.Join(rest[i:], " ")
			} else if spec.Required {
				return nil, fmt.Errorf("%w: %s — usage: %s", ErrMissingArg, spec.Name, def.Usage())
			}
			break
		}
		if i < len(rest) {
			args[spec.Name] = rest[i]
		} else if spec.Required {
			return nil, fmt.Errorf("%w: %s — usage: %s", ErrMissingArg, spec.Name, def.Usage())
		}
	}
	return &ParseResult{Def: def, Args: args}, nil
}

// HelpText generates auto-documented help text.
// If cmdName is empty, lists all commands. If cmdName is set, shows detailed help for that command.
func (r *Registry) HelpText(cmdName string) string {
	if cmdName != "" {
		def, ok := r.cmds[cmdName]
		if !ok {
			return fmt.Sprintf("Unknown command: %s", cmdName)
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Usage: %s\n", def.Usage()))
		sb.WriteString(fmt.Sprintf("  %s\n", def.Description))
		if len(def.Args) > 0 {
			sb.WriteString("Arguments:\n")
			for _, a := range def.Args {
				req := "optional"
				if a.Required {
					req = "required"
				}
				sb.WriteString(fmt.Sprintf("  %-12s %s (%s)\n", a.Name, a.Description, req))
			}
		}
		return strings.TrimRight(sb.String(), "\n")
	}

	var sb strings.Builder
	sb.WriteString("Available commands:\n")
	for _, name := range r.order {
		def := r.cmds[name]
		sb.WriteString(fmt.Sprintf("  %-16s %s\n", def.Usage(), def.Description))
	}
	return strings.TrimRight(sb.String(), "\n")
}
