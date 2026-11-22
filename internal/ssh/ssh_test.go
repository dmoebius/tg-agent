package ssh

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSSH puts an ssh on PATH that prints its arguments (one per line, after
// "args:") and its stdin (after "stdin:"), then exits with $FAKE_SSH_EXIT.
func fakeSSH(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
echo args:
for a in "$@"; do echo "$a"; done
echo stdin:
cat
exit "${FAKE_SSH_EXIT:-0}"
`
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

var client = Client{Host: "192.168.122.5", Key: "/data/id_ed25519", KnownHosts: "/data/known_hosts"}

// parse splits the fake's output into its arguments and stdin.
func parse(t *testing.T, out string) (args []string, stdin string) {
	t.Helper()
	a, in, ok := strings.Cut(out, "stdin:\n")
	if !ok || !strings.HasPrefix(a, "args:\n") {
		t.Fatalf("unexpected output %q", out)
	}
	return strings.Split(strings.TrimSuffix(strings.TrimPrefix(a, "args:\n"), "\n"), "\n"), in
}

func TestRun(t *testing.T) {
	fakeSSH(t)
	var out bytes.Buffer
	if err := client.Run(context.Background(), &out, strings.NewReader("payload"), "cat > f"); err != nil {
		t.Fatal(err)
	}
	args, stdin := parse(t, out.String())
	if stdin != "payload" {
		t.Errorf("stdin = %q", stdin)
	}
	n := len(args)
	if n < 2 || args[n-2] != "agent@192.168.122.5" || args[n-1] != "cat > f" {
		t.Errorf("the command must follow the destination as one argument: %q", args)
	}
	for _, want := range []string{"/data/id_ed25519", "UserKnownHostsFile=/data/known_hosts", "StrictHostKeyChecking=accept-new"} {
		if !contains(args, want) {
			t.Errorf("missing %q in %q", want, args)
		}
	}
}

func TestOutputExitStatus(t *testing.T) {
	fakeSSH(t)
	t.Setenv("FAKE_SSH_EXIT", "3")
	out, err := client.Output(context.Background(), "false")
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 3 {
		t.Fatalf("want *exec.ExitError with code 3, got %v", err)
	}
	if !strings.Contains(out, "agent@192.168.122.5") {
		t.Errorf("output not returned on failure: %q", out)
	}
}

func TestExec(t *testing.T) {
	fakeSSH(t)
	// Exec writes to os.Stdout; redirect it to a file to inspect the args.
	run := func(tty bool, cmd string) []string {
		t.Helper()
		f, err := os.CreateTemp(t.TempDir(), "out")
		if err != nil {
			t.Fatal(err)
		}
		stdout, stdin := os.Stdout, os.Stdin
		os.Stdout, os.Stdin = f, nil
		err = client.Exec(context.Background(), tty, cmd)
		os.Stdout, os.Stdin = stdout, stdin
		if err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(f.Name())
		args, _ := parse(t, string(b))
		return args
	}

	args := run(false, "")
	if args[0] != "-T" {
		t.Errorf("without tty the first flag must be -T: %q", args)
	}
	if args[len(args)-1] != "agent@192.168.122.5" {
		t.Errorf("an empty command must not add an argument (login shell): %q", args)
	}

	args = run(true, "ls -la")
	if args[0] != "-t" || args[len(args)-1] != "ls -la" {
		t.Errorf("with tty: %q", args)
	}

	t.Setenv("FAKE_SSH_EXIT", "42")
	stdout := os.Stdout
	os.Stdout, _ = os.Open(os.DevNull)
	err := client.Exec(context.Background(), false, "exit 42")
	os.Stdout = stdout
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 42 {
		t.Fatalf("want *exec.ExitError with code 42, got %v", err)
	}
}

func TestCommandUsesOwnKnownHosts(t *testing.T) {
	got := client.Command()
	want := "ssh -i /data/id_ed25519 -o IdentitiesOnly=yes -o UserKnownHostsFile=/data/known_hosts agent@192.168.122.5"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func contains(args []string, s string) bool {
	for _, a := range args {
		if strings.Contains(a, s) {
			return true
		}
	}
	return false
}
