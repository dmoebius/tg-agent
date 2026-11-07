package vm

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/dmoebius/tg-agent/internal/config"
)

// fakeVM records every command and answers by command prefix.
type fakeVM struct {
	cmds    []string
	stdin   map[string]string // cmd -> stdin it received
	outputs map[string]string // prefix -> output
	fail    map[string]int    // prefix -> number of calls that fail
	calls   map[string]int    // prefix -> calls so far
}

func newFake() *fakeVM {
	return &fakeVM{stdin: map[string]string{}, outputs: map[string]string{}, fail: map[string]int{}, calls: map[string]int{}}
}

func (f *fakeVM) answer(cmd string) (string, error) {
	f.cmds = append(f.cmds, cmd)
	for p, n := range f.fail {
		if strings.HasPrefix(cmd, p) {
			f.calls[p]++
			if f.calls[p] <= n {
				return "", errors.New("exit status 1")
			}
		}
	}
	for p, out := range f.outputs {
		if strings.HasPrefix(cmd, p) {
			return out, nil
		}
	}
	return "", nil
}

func (f *fakeVM) Run(_ context.Context, out io.Writer, stdin io.Reader, cmd string) error {
	if stdin != nil {
		b, _ := io.ReadAll(stdin)
		f.stdin[cmd] = string(b)
	}
	s, err := f.answer(cmd)
	io.WriteString(out, s)
	return err
}

func (f *fakeVM) Output(_ context.Context, cmd string) (string, error) { return f.answer(cmd) }

func (f *fakeVM) Interactive(_ context.Context, cmd string) error {
	f.cmds = append(f.cmds, cmd)
	return nil
}

func (f *fakeVM) Command() string { return "ssh agent@vm" }

func conn(f *fakeVM) *Conn { return &Conn{Host: "10.0.0.2", r: f, out: io.Discard} }

func TestUpdateMergesIntoCurrentTakopiTomlAndRestarts(t *testing.T) {
	f := newFake()
	f.outputs["cat ~/.takopi/takopi.toml"] = "[projects.foo]\npath = \"/home/agent/work/foo\"\n"
	err := conn(f).Update(context.Background(), config.Telegram{BotToken: "tok", ChatID: -100, AllowedUserIDs: []int64{1}})
	if err != nil {
		t.Fatal(err)
	}
	var toml, script string
	for cmd, in := range f.stdin {
		switch {
		case strings.Contains(cmd, "/opt/tg-agent/takopi.toml"):
			toml = in
		case strings.Contains(cmd, "/opt/tg-agent/provision.sh"):
			script = in
		}
	}
	if !strings.Contains(toml, "projects.foo") || !strings.Contains(toml, `bot_token = "tok"`) {
		t.Errorf("uploaded takopi.toml:\n%s", toml)
	}
	if !strings.HasPrefix(script, "#!") {
		t.Errorf("uploaded provision.sh starts with %.20q", script)
	}
	last := f.cmds[len(f.cmds)-1]
	if last != restartTakopi {
		t.Errorf("last command %q, want restart", last)
	}
	if i, j := indexOf(f.cmds, "sudo bash /opt/tg-agent/provision.sh"), indexOf(f.cmds, "sudo DEBIAN"); i < 0 || j < 0 || j > i {
		t.Errorf("want apt before provision, got %q", f.cmds)
	}
}

func TestUpdateStopsAtFirstFailingStep(t *testing.T) {
	f := newFake()
	f.fail["sudo DEBIAN"] = 1
	err := conn(f).Update(context.Background(), config.Telegram{})
	if err == nil || !strings.HasPrefix(err.Error(), "apt:") {
		t.Fatalf("err = %v", err)
	}
	if indexOf(f.cmds, restartTakopi) >= 0 {
		t.Error("restarted Takopi after a failed step")
	}
}

func TestEnsureClaudeAuth(t *testing.T) {
	t.Run("already logged in", func(t *testing.T) {
		f := newFake()
		if err := conn(f).EnsureClaudeAuth(context.Background(), true); err != nil {
			t.Fatal(err)
		}
		if len(f.cmds) != 1 {
			t.Errorf("cmds = %q", f.cmds)
		}
	})
	t.Run("not interactive", func(t *testing.T) {
		f := newFake()
		f.fail[claudeAuthStatus] = 1
		err := conn(f).EnsureClaudeAuth(context.Background(), false)
		if err == nil || !strings.Contains(err.Error(), "ssh agent@vm") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("login then restart", func(t *testing.T) {
		f := newFake()
		f.fail[claudeAuthStatus] = 1 // fails once, succeeds after login
		if err := conn(f).EnsureClaudeAuth(context.Background(), true); err != nil {
			t.Fatal(err)
		}
		if indexOf(f.cmds, "bash -lc 'claude auth login") < 0 || f.cmds[len(f.cmds)-1] != restartTakopi {
			t.Errorf("cmds = %q", f.cmds)
		}
	})
	t.Run("login aborted", func(t *testing.T) {
		f := newFake()
		f.fail[claudeAuthStatus] = 2
		if err := conn(f).EnsureClaudeAuth(context.Background(), true); err == nil {
			t.Fatal("expected error")
		}
		if indexOf(f.cmds, restartTakopi) >= 0 {
			t.Error("restarted Takopi without login")
		}
	})
}

func TestTakopiStatus(t *testing.T) {
	f := newFake()
	f.outputs["systemctl is-active takopi"] = "active\n"
	if got := conn(f).TakopiStatus(context.Background()); got != "active" {
		t.Fatalf("got %q", got)
	}
}

func indexOf(cmds []string, prefix string) int {
	for i, c := range cmds {
		if strings.HasPrefix(c, prefix) {
			return i
		}
	}
	return -1
}
