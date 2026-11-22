package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/dmoebius/tg-agent/internal/prompt"
	"github.com/dmoebius/tg-agent/internal/vm"
)

func TestExitCode(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		code int
		show bool
	}{
		"success":     {nil, 0, false},
		"remote exit": {fmt.Errorf("shell: %w", &vm.ExitError{Code: 42}), 42, false},
		"ctrl-c":      {fmt.Errorf("setup: %w", prompt.ErrInterrupted), 130, false},
		"other":       {errors.New("boom"), 1, true},
	} {
		if code, show := exitCode(tc.err); code != tc.code || show != tc.show {
			t.Errorf("%s: got (%d, %v), want (%d, %v)", name, code, show, tc.code, tc.show)
		}
	}
}

// TestCommands checks the command tree: names, aliases, flags and argument
// handling, without running any command.
func TestCommands(t *testing.T) {
	root := newRootCmd()
	flags := map[string][]string{
		"start":  nil,
		"stop":   nil,
		"status": nil,
		"update": nil,
		"shell":  nil,
		"delete": {"wipe-disk", "wipe-all"},
		"doctor": {"fix"},
	}
	for name, want := range flags {
		c, _, err := root.Find([]string{name})
		if err != nil || c.Name() != name {
			t.Errorf("command %s not found: %v", name, err)
			continue
		}
		if c.RunE == nil {
			t.Errorf("%s: no RunE", name)
		}
		for _, f := range want {
			if c.Flags().Lookup(f) == nil {
				t.Errorf("%s: flag --%s missing", name, f)
			}
		}
		if name == "shell" {
			continue
		}
		if c.Args == nil || c.Args(c, []string{"extra"}) == nil {
			t.Errorf("%s: accepts arguments", name)
		}
	}
	for _, c := range root.Commands() {
		if _, ok := flags[c.Name()]; !ok && c.Name() != "help" && c.Name() != "completion" {
			t.Errorf("command %s is not covered by this test", c.Name())
		}
	}

	sh, _, err := root.Find([]string{"sh"})
	if err != nil || sh.Name() != "shell" {
		t.Errorf("sh is not an alias of shell: %v", err)
	}
	if !sh.DisableFlagParsing {
		t.Error("shell must pass flags through to the remote command")
	}
}

// TestHelp renders the help of every command, which also makes Cobra check
// the flag definitions (e.g. duplicate shorthands panic).
func TestHelp(t *testing.T) {
	root := newRootCmd()
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		c.SetOut(io.Discard)
		if err := c.Help(); err != nil {
			t.Errorf("%s: %v", c.CommandPath(), err)
		}
		if strings.TrimSpace(c.Short) == "" && c.Name() != "help" && c.Name() != "completion" {
			t.Errorf("%s: no short description", c.CommandPath())
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	walk(root)
}
