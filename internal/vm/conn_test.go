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
	if p, r := indexOf(f.cmds, "sudo bash /opt/tg-agent/provision.sh"), indexOf(f.cmds, restartTakopi); p < 0 || r < p {
		t.Errorf("want provisioning, then restart: %q", f.cmds)
	}
}

func TestUpdateReportsPendingReboot(t *testing.T) {
	var out strings.Builder
	f := newFake()
	f.fail["test -e /var/run/reboot-required"] = 1
	c := &Conn{r: f, out: &out}
	if err := c.Update(context.Background(), config.Telegram{}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out.String(), "\nUpdate complete.\n") {
		t.Errorf("no completion message at the end:\n%s", out.String())
	}
	if strings.Contains(out.String(), "reboot") {
		t.Errorf("reboot hint without pending reboot:\n%s", out.String())
	}
	out.Reset()
	if err := (&Conn{r: newFake(), out: &out}).Update(context.Background(), config.Telegram{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "tg-agent stop && tg-agent start") {
		t.Errorf("no reboot hint:\n%s", out.String())
	}
}

func TestUpdateStopsAtFirstFailingStep(t *testing.T) {
	f := newFake()
	f.fail["sudo bash /opt/tg-agent/provision.sh"] = 1
	err := conn(f).Update(context.Background(), config.Telegram{})
	if err == nil || !strings.HasPrefix(err.Error(), "provision:") {
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

func TestWaitProvisioned(t *testing.T) {
	f := newFake()
	if err := conn(f).WaitProvisioned(context.Background()); err != nil {
		t.Fatal(err)
	}
	if indexOf(f.cmds, "cloud-init status --long") >= 0 {
		t.Errorf("detailed status printed on success: %q", f.cmds)
	}
	f = newFake()
	f.fail["cloud-init status --wait"] = 1
	if err := conn(f).WaitProvisioned(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if indexOf(f.cmds, "cloud-init status --long") < 0 {
		t.Errorf("no detailed status on failure: %q", f.cmds)
	}
}

func TestTakopiStatus(t *testing.T) {
	cases := []struct{ out, want string }{
		// Up for 2 hours: healthy, old restarts are irrelevant.
		{"ActiveState=active\nNRestarts=210\nActiveEnterTimestampMonotonic=100000000\n7300.50 1000.00\n", "active (for 2h0m0s)"},
		// Just (re)started after crashes: may be a crash loop.
		{"ActiveState=active\nNRestarts=106\nActiveEnterTimestampMonotonic=7295000000\n7300.50 1000.00\n", "active (restarted 106 times"},
		{"ActiveState=activating\nNRestarts=106\nActiveEnterTimestampMonotonic=0\n7300.50 1000.00\n", "activating (restarted 106 times"},
		{"ActiveState=active\nNRestarts=0\nActiveEnterTimestampMonotonic=7295000000\n7300.50 1000.00\n", "active"},
		{"", "unknown"},
	}
	for _, tc := range cases {
		if got := takopiStatus(tc.out); !strings.HasPrefix(got, tc.want) || (tc.want == "active" && got != "active") {
			t.Errorf("takopiStatus(%q) = %q, want prefix %q", tc.out, got, tc.want)
		}
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
