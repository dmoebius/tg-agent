// Package preflight verifies the host has the libvirt/qemu tooling tg-agent needs.
package preflight

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type tool struct{ bin, apt, dnf, pacman string }

// Required host tools and the packages providing each (Debian/Ubuntu, Fedora, Arch).
var tools = []tool{
	{"virsh", "libvirt-clients", "libvirt-client", "libvirt"},
	{"virt-install", "virtinst", "virt-install", "virt-install"},
	{"qemu-img", "qemu-utils", "qemu-img", "qemu-img"},
	{"ssh", "openssh-client", "openssh-clients", "openssh"},
	{"ssh-keygen", "openssh-client", "openssh-clients", "openssh"},
}

// MissingTools returns "bin (apt: .. | dnf: .. | pacman: ..)" entries for tools not found via lookPath.
func MissingTools(lookPath func(string) (string, error)) []string {
	var out []string
	for _, t := range tools {
		if _, err := lookPath(t.bin); err != nil {
			out = append(out, fmt.Sprintf("%s (apt: %s | dnf: %s | pacman: %s)", t.bin, t.apt, t.dnf, t.pacman))
		}
	}
	return out
}

const daemonHint = `hint: install and start the libvirt daemon, then add your user to the 'libvirt' group
      apt:    apt install libvirt-daemon-system qemu-system-x86 dnsmasq-base
      dnf:    dnf install libvirt-daemon-kvm libvirt-daemon-config-network
      pacman: pacman -S libvirt qemu-base dnsmasq
      systemctl enable --now libvirtd   (Arch: libvirtd.socket)`

// Check returns a human-readable error listing everything that is missing or misconfigured.
func Check(ctx context.Context, uri, network string) error {
	var probs []string
	if m := MissingTools(exec.LookPath); len(m) > 0 {
		probs = append(probs, "missing tools: "+strings.Join(m, ", "))
	}
	if len(probs) == 0 {
		if out, err := exec.CommandContext(ctx, "virsh", "-c", uri, "list").CombinedOutput(); err != nil {
			probs = append(probs, fmt.Sprintf("cannot connect to libvirt (%s): %s\n    %s",
				uri, strings.TrimSpace(string(out)), daemonHint))
		} else if out, err := exec.CommandContext(ctx, "virsh", "-c", uri, "net-info", network).CombinedOutput(); err != nil {
			probs = append(probs, fmt.Sprintf("libvirt network %q not available: %s\n    hint: virsh -c %s net-start %s; virsh -c %s net-autostart %s",
				network, strings.TrimSpace(string(out)), uri, network, uri, network))
		}
	}
	if _, err := os.Stat("/dev/kvm"); err != nil {
		probs = append(probs, "/dev/kvm not found: enable virtualization (VT-x/AMD-V) and load the kvm module")
	}
	if len(probs) > 0 {
		return fmt.Errorf("preflight failed:\n  - %s", strings.Join(probs, "\n  - "))
	}
	return nil
}
