package vm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/dmoebius/tg-agent/internal/config"
	"github.com/dmoebius/tg-agent/internal/provision"
)

// runner executes shell commands in the VM. ssh.Client is the real adapter;
// tests use a fake.
type runner interface {
	Run(ctx context.Context, out io.Writer, stdin io.Reader, cmd string) error
	Output(ctx context.Context, cmd string) (string, error)
	Interactive(ctx context.Context, cmd string) error
	Command() string
}

// Conn is a connection into the running VM. It knows what tg-agent does
// inside the VM: waiting for Provisioning, logging Claude in, Updates and
// Takopi's service status.
type Conn struct {
	Host string
	r    runner
	out  io.Writer
}

const (
	claudeAuthStatus = `bash -lc 'claude auth status --text'`
	restartTakopi    = "sudo systemctl restart takopi"
)

// SSHCommand returns the ssh command line users can paste to log in.
func (c *Conn) SSHCommand() string { return c.r.Command() }

// WaitProvisioned blocks until the initial Provisioning (cloud-init) of a
// new VM has finished; it returns at once on later boots. It prints only a
// progress line, plus cloud-init's detailed status if Provisioning failed.
func (c *Conn) WaitProvisioned(ctx context.Context) error {
	if err := c.r.Run(ctx, c.out, nil, "cloud-init status --wait"); err != nil {
		_ = c.r.Run(ctx, c.out, nil, "cloud-init status --long")
		return fmt.Errorf("provisioning failed, see /var/log/cloud-init-output.log in the VM: %w", err)
	}
	return nil
}

// EnsureClaudeAuth logs Claude in (subscription) if it is not yet, then
// restarts Takopi so it picks up the credentials. The login needs the user's
// terminal; without one (interactive false) it returns instructions instead.
func (c *Conn) EnsureClaudeAuth(ctx context.Context, interactive bool) error {
	if _, err := c.r.Output(ctx, claudeAuthStatus); err == nil {
		return nil
	}
	if !interactive {
		return fmt.Errorf("Claude is not logged in; run `%s` and then `claude auth login`", c.r.Command())
	}
	fmt.Fprintln(c.out, "Claude is not logged in. Starting `claude auth login` (Claude subscription) ...")
	if err := c.r.Interactive(ctx, `bash -lc 'claude auth login --claudeai'`); err != nil {
		return fmt.Errorf("claude auth login: %w", err)
	}
	if _, err := c.r.Output(ctx, claudeAuthStatus); err != nil {
		return errors.New("Claude login did not complete")
	}
	_, err := c.r.Output(ctx, restartTakopi)
	return err
}

// TakopiStatus returns the state of the Takopi service, e.g. "active (for
// 2h5m0s)". A crashing service is "active" for a few seconds between
// restarts, so it only counts as healthy once it has been up for a while;
// otherwise the number of restarts is included.
func (c *Conn) TakopiStatus(ctx context.Context) string {
	out, _ := c.r.Output(ctx, "systemctl show takopi -p ActiveState -p NRestarts -p ActiveEnterTimestampMonotonic; cat /proc/uptime")
	return takopiStatus(out)
}

func takopiStatus(out string) string {
	props := map[string]string{}
	var uptime float64
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if k, v, ok := strings.Cut(l, "="); ok {
			props[k] = v
		} else if f := strings.Fields(l); len(f) == 2 {
			uptime, _ = strconv.ParseFloat(f[0], 64)
		}
	}
	state := props["ActiveState"]
	if state == "" {
		return "unknown"
	}
	enteredUS, _ := strconv.ParseFloat(props["ActiveEnterTimestampMonotonic"], 64)
	up := time.Duration(uptime-enteredUS/1e6) * time.Second
	if state == "active" && enteredUS > 0 && up >= 30*time.Second {
		return fmt.Sprintf("active (for %s)", up)
	}
	if n := props["NRestarts"]; n != "" && n != "0" {
		state += fmt.Sprintf(" (restarted %s times, see `sudo journalctl -u takopi`)", n)
	}
	return state
}

// Update pushes tg into the VM's takopi.toml (keeping everything Takopi
// owns), re-runs Provisioning (which upgrades the system packages and
// installs or upgrades every tool) and restarts Takopi. It tells the user
// when the VM needs a reboot afterwards.
func (c *Conn) Update(ctx context.Context, tg config.Telegram) error {
	current, err := c.r.Output(ctx, "cat ~/.takopi/takopi.toml 2>/dev/null || true")
	if err != nil {
		return fmt.Errorf("read takopi.toml: %w", err)
	}
	files, err := provision.Files(tg, current)
	if err != nil {
		return err
	}
	type step struct{ desc, cmd, stdin string }
	var steps []step
	for _, f := range files {
		steps = append(steps, step{"upload " + f.Path, upload(f), f.Content})
	}
	steps = append(steps,
		step{"provision", "sudo " + provision.Command, ""},
		step{"restart", restartTakopi, ""},
	)
	for _, s := range steps {
		fmt.Fprintf(c.out, "==> %s\n", s.desc)
		var in io.Reader
		if s.stdin != "" {
			in = strings.NewReader(s.stdin)
		}
		if err := c.r.Run(ctx, c.out, in, s.cmd); err != nil {
			return fmt.Errorf("%s: %w", s.desc, err)
		}
	}
	fmt.Fprintln(c.out, "\nUpdate complete.")
	if _, err := c.r.Output(ctx, "test -e /var/run/reboot-required"); err == nil {
		fmt.Fprintln(c.out, "The VM needs a reboot to complete the update (e.g. a new kernel): run `tg-agent stop && tg-agent start`.")
	}
	return nil
}

// upload returns a command that writes stdin to f.Path with f.Mode, without
// the content ever being readable by others.
func upload(f provision.File) string {
	return fmt.Sprintf("sudo install -d -m 700 %s && sudo sh -c 'umask 077 && cat > %s && chmod %#o %s'",
		provision.Dir, f.Path, f.Mode, f.Path)
}
