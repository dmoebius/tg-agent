package vm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// Forwarding is what tg-agent can tell without root about whether the Host
// forwards the VM's traffic to the internet.
type Forwarding struct {
	Bridge    string // bridge of the VM's libvirt network, e.g. virbr0
	IPForward bool   // net.ipv4.ip_forward
	Docker    bool   // the Docker daemon runs
}

var bridgeRe = regexp.MustCompile(`(?m)^Bridge:\s+([A-Za-z0-9_.-]+)\s*$`)

// Forwarding inspects the Host side of the VM's network.
func (m *Manager) Forwarding(ctx context.Context) (Forwarding, error) {
	var f Forwarding
	out, err := m.virsh(ctx, "net-info", m.Cfg.VM.Network)
	if err != nil {
		return f, err
	}
	mm := bridgeRe.FindStringSubmatch(out)
	if mm == nil {
		return f, fmt.Errorf("libvirt network %q has no bridge", m.Cfg.VM.Network)
	}
	f.Bridge = mm[1]
	b, _ := os.ReadFile("/proc/sys/net/ipv4/ip_forward")
	f.IPForward = strings.TrimSpace(string(b)) == "1"
	f.Docker = exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", "docker").Run() == nil
	return f, nil
}

// ErrNoDockerChain means Docker's DOCKER-USER chain does not exist, so
// Docker's firewall is not what blocks the VM.
var ErrNoDockerChain = errors.New("iptables chain DOCKER-USER not found")

// sudo runs a command on the Host as root with the user's terminal attached,
// so sudo can ask for the password. It first prints the exact command, so
// the user sees what they authorize. quiet drops the command's output, for
// commands run only for their exit status.
func (m *Manager) sudo(ctx context.Context, quiet bool, args ...string) error {
	fmt.Fprintln(m.Out, "$ "+m.commandLine("sudo", args))
	cmd := exec.CommandContext(ctx, "sudo", args...)
	cmd.Stdin = os.Stdin
	if !quiet {
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	}
	return cmd.Run()
}

// commandLine renders a command as it could be pasted into a shell, with the
// Config's secrets redacted.
func (m *Manager) commandLine(name string, args []string) string {
	words := []string{name}
	for _, a := range args {
		if t := m.Cfg.Telegram.BotToken; t != "" {
			a = strings.ReplaceAll(a, t, "[REDACTED]")
		}
		words = append(words, shellQuote(a))
	}
	return strings.Join(words, " ")
}

// EnableIPForward turns on IPv4 forwarding on the Host (until reboot; libvirt
// normally enables it when it starts the network). Needs sudo.
func (m *Manager) EnableIPForward(ctx context.Context) error {
	return m.sudo(ctx, false, "sysctl", "-q", "-w", "net.ipv4.ip_forward=1")
}

// AllowForwardThroughDocker makes Docker's firewall accept the traffic of the
// VM's bridge. Docker sets the policy of the iptables FORWARD chain to DROP
// and only accepts its own bridges. libvirt's nftables rules live in a
// separate table and cannot override that: a packet must pass every chain
// hooked into forward. DOCKER-USER is the chain Docker reserves for such
// exceptions; libvirt's own rules still apply afterwards. The rules last
// until the next reboot. Needs sudo.
func (m *Manager) AllowForwardThroughDocker(ctx context.Context, bridge string) error {
	// Ask for the password first, visibly; the probes below are quiet.
	if err := m.sudo(ctx, false, "-v"); err != nil {
		return fmt.Errorf("sudo: %w", err)
	}
	if m.sudo(ctx, true, "iptables", "-w", "-n", "-L", "DOCKER-USER") != nil {
		return ErrNoDockerChain
	}
	for _, rule := range dockerUserRules(bridge) {
		if m.sudo(ctx, true, append([]string{"iptables", "-w", "-C", "DOCKER-USER"}, rule...)...) == nil {
			continue // already there
		}
		if err := m.sudo(ctx, false, append([]string{"iptables", "-w", "-I", "DOCKER-USER"}, rule...)...); err != nil {
			return fmt.Errorf("iptables -I DOCKER-USER %s: %w", strings.Join(rule, " "), err)
		}
	}
	return nil
}

// dockerUserRules accepts what leaves the bridge and the replies to it.
func dockerUserRules(bridge string) [][]string {
	return [][]string{
		{"-i", bridge, "-j", "ACCEPT"},
		{"-o", bridge, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
	}
}
