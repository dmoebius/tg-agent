package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/dmoebius/tg-agent/internal/config"
	"github.com/dmoebius/tg-agent/internal/preflight"
	"github.com/dmoebius/tg-agent/internal/takopi"
	"github.com/dmoebius/tg-agent/internal/telegram"
	"github.com/dmoebius/tg-agent/internal/vm"
)

// Doctor checks the whole path from Telegram to Claude and reports what is
// wrong; with fix it also repairs what it can. It returns an error if
// problems remain.
func Doctor(ctx context.Context, fix bool) error {
	d := &doctor{fix: fix}
	if err := d.run(ctx); err != nil {
		return err
	}
	fmt.Println()
	if d.problems == 0 {
		fmt.Println("No problems found.")
		return nil
	}
	if !fix {
		fmt.Println("Run `tg-agent doctor --fix` to repair what can be repaired automatically.")
	}
	return fmt.Errorf("%d problem(s) found", d.problems)
}

// doctor runs the checks in order, from Telegram towards Claude, and counts
// the problems that remain.
type doctor struct {
	fix      bool
	problems int
}

func (d *doctor) ok(format string, a ...any) { fmt.Printf("[ ok ] "+format+"\n", a...) }

// info reports something worth knowing that is not a problem.
func (d *doctor) info(format string, a ...any) { fmt.Printf("[info] "+format+"\n", a...) }

func (d *doctor) fail(format string, a ...any) {
	d.problems++
	fmt.Printf("[FAIL] "+format+"\n", a...)
}

// fixed reports that the problem reported by the preceding fail is gone.
func (d *doctor) fixed(format string, a ...any) {
	d.problems--
	fmt.Printf("[fixed] "+format+"\n", a...)
}

// hint adds an explanation or advice to the line above.
func (d *doctor) hint(format string, a ...any) {
	fmt.Println("       " + strings.ReplaceAll(fmt.Sprintf(format, a...), "\n", "\n       "))
}

func (d *doctor) run(ctx context.Context) error {
	// No setup(): doctor must not start the interactive Telegram setup.
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		d.fail("Config %s: %v", config.Path(), err)
		d.hint("Run `tg-agent start` to set it up.")
		return nil
	}
	d.ok("Config %s", config.Path())

	tg := telegram.Client{Token: cfg.Telegram.BotToken}
	pending := d.checkTelegram(ctx, tg, cfg.Telegram)

	if err := preflight.Check(ctx, cfg.VM.LibvirtURI, cfg.VM.Network); err != nil {
		d.fail("%v", err)
		return nil
	}
	m := &vm.Manager{Cfg: cfg, Out: os.Stdout}
	c := d.checkVM(ctx, m)
	if c == nil {
		return nil
	}

	netOK, netFixed := d.checkNet(ctx, m, c)
	restart := netFixed // Takopi retries only every two minutes
	takopiProblems := 0 // failures a restart of Takopi can cure

	status := c.TakopiStatus(ctx)
	if strings.HasPrefix(status, "active") {
		d.ok("Takopi service: %s", status)
	} else {
		takopiProblems++
		d.fail("Takopi service: %s", status)
		d.hint("Log: `tg-agent sh sudo journalctl -u takopi -n 50`")
		restart = true
	}

	d.checkTakopiConfig(ctx, c, cfg.Telegram)

	if c.ClaudeLoggedIn(ctx) {
		d.ok("Claude is logged in")
	} else {
		d.fail("Claude is not logged in in the VM")
		d.hint("Run `tg-agent start` to log in.")
	}

	// A failing Takopi stays "active", so only its log shows whether it
	// reaches Telegram. Without network the errors are just a consequence
	// and not worth a restart.
	const minutes = 10
	n, last := c.TakopiErrors(ctx, fmt.Sprintf("-%dmin", minutes))
	if n == 0 {
		d.ok("Takopi log: no errors in the last %d minutes", minutes)
	} else {
		takopiProblems++
		d.fail("Takopi log: %d error(s) in the last %d minutes, the last one:", n, minutes)
		d.hint("%s", last)
		if !netOK {
			d.hint("Expected while the VM cannot reach Telegram; fix the network first.")
		} else {
			d.hint("Log: `tg-agent sh sudo journalctl -u takopi -n 50`")
			restart = true
		}
	}

	if !d.fix || !restart || !netOK {
		return nil
	}
	fmt.Println("Restarting Takopi ...")
	if err := c.RestartTakopi(ctx); err != nil {
		d.fail("restart Takopi: %v", err)
		return nil
	}
	// Give it time to connect and fetch what is waiting.
	const settle = 10 * time.Second
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(settle):
	}
	status = c.TakopiStatus(ctx)
	errs, last := c.TakopiErrors(ctx, fmt.Sprintf("-%ds", int(settle.Seconds())))
	if !strings.HasPrefix(status, "active") || errs > 0 {
		if takopiProblems == 0 {
			d.problems++
		}
		fmt.Printf("[FAIL] Takopi still fails after the restart (service: %s)\n", status)
		if last != "" {
			d.hint("%s", last)
		}
		return nil
	}
	// The restart cured the service and log failures reported above.
	d.problems -= takopiProblems
	fmt.Println("[fixed] Takopi restarted and runs without errors")
	if pending > 0 {
		if w, err := tg.Webhook(ctx); err == nil && w.Pending == 0 {
			d.info("Takopi fetched the %d waiting message(s)", pending)
		}
	}
	return nil
}

// checkTelegram verifies, from the Host, the bot token, the bot's rights in
// the Forum and that updates are delivered by polling. It returns the number
// of updates waiting to be fetched.
func (d *doctor) checkTelegram(ctx context.Context, tg telegram.Client, t config.Telegram) (pending int) {
	title, err := tg.CheckForum(ctx, t.ChatID)
	if err != nil {
		d.fail("Telegram: %v", err)
		return 0
	}
	d.ok("Telegram: bot may manage topics in forum %q", title)

	w, err := tg.Webhook(ctx)
	if err != nil {
		d.fail("Telegram: %v", err)
		return 0
	}
	if w.URL != "" {
		d.fail("Telegram: the bot has a webhook set, so Takopi cannot poll for messages")
		if d.fix {
			if err := tg.DeleteWebhook(ctx); err != nil {
				d.hint("removing it failed: %v", err)
			} else {
				d.fixed("webhook removed")
			}
		} else {
			d.hint("Something else uses this bot token. --fix removes the webhook.")
		}
	}
	if w.Pending > 0 {
		d.info("Telegram: %d message(s) are waiting, Takopi has not fetched them yet", w.Pending)
	}
	return w.Pending
}

// checkVM returns a connection into the running VM, or nil if there is none.
func (d *doctor) checkVM(ctx context.Context, m *vm.Manager) *vm.Conn {
	st, err := m.State(ctx)
	if err != nil {
		d.fail("VM: %v", err)
		return nil
	}
	timeout := time.Duration(0)
	switch {
	case st == vm.Running:
		d.ok("VM is running")
	case st == vm.Stopped && d.fix:
		d.fail("VM is %s", st)
		if err := m.EnsureRunning(ctx); err != nil {
			d.hint("starting it failed: %v", err)
			return nil
		}
		d.fixed("VM started, waiting for it to boot ...")
		timeout = 5 * time.Minute
	default:
		d.fail("VM is %s", st)
		d.hint("Run `tg-agent start`.")
		return nil
	}
	c, err := m.Connect(ctx, timeout)
	if err != nil {
		d.fail("SSH into the VM: %v", err)
		d.hint("The VM may still be booting, or hang: try `tg-agent restart`.")
		return nil
	}
	d.ok("SSH into the VM (%s)", c.Host)
	return c
}

// checkNet walks the VM's network from the nearest hop to Telegram and stops
// at the first one that fails, because everything behind it fails too. ok
// reports whether the VM reaches Telegram (in the end), fixed whether that
// took a repair.
func (d *doctor) checkNet(ctx context.Context, m *vm.Manager, c *vm.Conn) (ok, fixed bool) {
	n := c.Net(ctx)
	if n.Gateway == "" || !n.GatewayOK {
		d.fail("VM network: no connection to the host (gateway %q)", n.Gateway)
		d.hint("Restart the VM: `tg-agent restart`.")
		return false, false
	}
	d.ok("VM network: reaches the host (gateway %s)", n.Gateway)

	if !n.Internet {
		d.fail("VM network: no internet, the host does not forward the VM's traffic")
		if !d.fixForwarding(ctx, m) {
			return false, false
		}
		if n = c.Net(ctx); !n.Internet {
			d.hint("Still no internet. Inspect the host's firewall: `sudo nft list ruleset`.")
			return false, false
		}
		d.fixed("VM network: internet reachable")
		fixed = true
	} else {
		d.ok("VM network: internet reachable")
	}

	if !n.DNSOK {
		d.fail("VM network: cannot resolve api.telegram.org")
		d.explainDNS(ctx, n.Gateway)
		return false, fixed
	}
	d.ok("VM network: DNS resolves api.telegram.org")

	if !n.Telegram {
		d.fail("VM network: api.telegram.org does not answer")
		if _, err := (telegram.Client{}).BotUsername(ctx); isNetworkError(err) {
			d.hint("The host cannot reach it either: Telegram may be down or blocked in this network.")
		} else {
			d.hint("The host reaches it, the VM does not: a VPN or firewall on the host may block the VM.")
		}
		return false, fixed
	}
	d.ok("VM network: reaches api.telegram.org")
	return true, fixed
}

// isNetworkError reports whether a Bot API call failed before Telegram
// answered. An answer, even an error like "Not Found", carries a description.
func isNetworkError(err error) bool {
	return err != nil && !strings.Contains(err.Error(), "Not Found") && !strings.Contains(err.Error(), "Unauthorized")
}

// fixForwarding explains why the Host does not forward the VM's traffic and,
// with --fix, repairs it. It reports whether it changed something.
func (d *doctor) fixForwarding(ctx context.Context, m *vm.Manager) bool {
	f, err := m.Forwarding(ctx)
	if err != nil {
		d.hint("%v", err)
		return false
	}
	changed := false
	if !f.IPForward {
		d.hint("IP forwarding is off on the host (net.ipv4.ip_forward = 0).")
		if d.fix {
			if err := m.EnableIPForward(ctx); err != nil {
				d.hint("enabling it failed: %v", err)
			} else {
				changed = true
			}
		}
	}
	if !f.Docker {
		if f.IPForward {
			d.hint("A firewall on the host probably drops traffic from %s. Inspect it: `sudo nft list ruleset`.", f.Bridge)
		}
		return changed
	}
	d.hint(`Docker runs on the host. Its firewall drops all forwarded traffic except that
of its own networks (iptables chain FORWARD, policy DROP), including the VM's on %s.
libvirt's own rules cannot override that.`, f.Bridge)
	if !d.fix {
		d.hint("--fix adds accept rules for %s to Docker's chain DOCKER-USER (needs sudo).", f.Bridge)
		return changed
	}
	fmt.Printf("Allowing traffic of %s through Docker's firewall (sudo) ...\n", f.Bridge)
	if err := m.AllowForwardThroughDocker(ctx, f.Bridge); err != nil {
		if errors.Is(err, vm.ErrNoDockerChain) {
			d.hint("Docker's chain DOCKER-USER does not exist, so something else blocks the VM. Inspect: `sudo nft list ruleset`.")
		} else {
			d.hint("failed: %v", err)
		}
		return changed
	}
	d.hint(`The rules last until the host reboots; run "tg-agent doctor --fix" again then.
For a permanent solution make libvirt use the same firewall as Docker: set
firewall_backend = "iptables" in /etc/libvirt/network.conf and restart
libvirtd (see https://libvirt.org/firewall.html).`)
	return true
}

// explainDNS tells the user where the VM's DNS queries go. The VM asks
// libvirt's dnsmasq on the Host, which forwards to the Host's nameservers;
// those change with every VPN connection.
func (d *doctor) explainDNS(ctx context.Context, gateway string) {
	lctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := net.DefaultResolver.LookupHost(lctx, "api.telegram.org")
	ns := strings.Join(hostNameservers(), ", ")
	if err != nil {
		d.hint("The host cannot resolve it either (%v).", err)
		d.hint("Check the nameservers in the host's /etc/resolv.conf: %s", ns)
		d.hint("After a VPN was connected or dropped they may be unreachable; reconnect the network or the VPN.")
		return
	}
	d.hint(`The host resolves it, the VM does not. The VM asks libvirt's dnsmasq on %s,
which forwards to the nameservers in the host's /etc/resolv.conf: %s
After a VPN change dnsmasq may still use nameservers that are no longer reachable.
Restart the libvirt network's DNS: sudo systemctl restart libvirtd`, gateway, ns)
}

// hostNameservers returns the nameservers in the Host's /etc/resolv.conf.
func hostNameservers() []string {
	b, _ := os.ReadFile("/etc/resolv.conf")
	return parseNameservers(string(b))
}

func parseNameservers(resolvConf string) []string {
	var out []string
	for _, l := range strings.Split(resolvConf, "\n") {
		if f := strings.Fields(l); len(f) >= 2 && f[0] == "nameserver" {
			out = append(out, f[1])
		}
	}
	return out
}

// checkTakopiConfig verifies the VM's takopi.toml carries the Config's
// Telegram settings.
func (d *doctor) checkTakopiConfig(ctx context.Context, c *vm.Conn, t config.Telegram) {
	current, err := c.TakopiConfig(ctx)
	if err != nil {
		d.fail("Takopi config: cannot read ~/.takopi/takopi.toml in the VM: %v", err)
		return
	}
	drift, err := takopi.Drift(current, t)
	if err != nil {
		d.fail("Takopi config: %v", err)
		return
	}
	if len(drift) > 0 {
		d.fail("Takopi config in the VM differs from %s in: %s", config.Path(), strings.Join(drift, ", "))
		d.hint("Run `tg-agent update` to push the config into the VM.")
		return
	}
	d.ok("Takopi config in the VM matches the config")
}
