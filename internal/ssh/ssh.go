// Package ssh runs commands in the VM via the system ssh client.
package ssh

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
)

type Client struct {
	Host, Key, KnownHosts string
}

func (c Client) args(extra ...string) []string {
	return append([]string{
		"-i", c.Key, "-o", "IdentitiesOnly=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile=" + c.KnownHosts,
		"-o", "ConnectTimeout=5", "-o", "LogLevel=ERROR",
		"agent@" + c.Host,
	}, extra...)
}

// Run executes cmd remotely, streaming output to out and feeding stdin (may be nil).
func (c Client) Run(ctx context.Context, out io.Writer, stdin io.Reader, cmd string) error {
	e := exec.CommandContext(ctx, "ssh", c.args(cmd)...)
	e.Stdout, e.Stderr, e.Stdin = out, out, stdin
	return e.Run()
}

// Interactive runs cmd with a TTY attached to the user's terminal.
func (c Client) Interactive(ctx context.Context, cmd string) error {
	e := exec.CommandContext(ctx, "ssh", append([]string{"-t"}, c.args(cmd)...)...)
	e.Stdin, e.Stdout, e.Stderr = os.Stdin, os.Stdout, os.Stderr
	return e.Run()
}

func (c Client) Output(ctx context.Context, cmd string) (string, error) {
	b, err := exec.CommandContext(ctx, "ssh", c.args(cmd)...).CombinedOutput()
	return string(b), err
}

// Command returns the interactive command line users can paste.
func (c Client) Command() string {
	return "ssh -i " + c.Key + " agent@" + c.Host
}

// Quote quotes s for safe use as a single word in a remote POSIX shell command.
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
