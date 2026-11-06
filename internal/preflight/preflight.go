// Package preflight verifies the host has the libvirt/qemu tooling tg-agent needs.
package preflight

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type tool struct{ bin, pkg string }

// Required host tools and the Debian/Ubuntu package providing each.
var tools = []tool{
	{"virsh", "libvirt-clients"},
	{"virt-install", "virtinst"},
	{"qemu-img", "qemu-utils"},
	{"ssh", "openssh-client"},
	{"ssh-keygen", "openssh-client"},
}

// MissingTools returns "bin (package)" entries for tools not found via lookPath.
func MissingTools(lookPath func(string) (string, error)) []string {
	var out []string
	for _, t := range tools {
		if _, err := lookPath(t.bin); err != nil {
			out = append(out, fmt.Sprintf("%s (apt: %s)", t.bin, t.pkg))
		}
	}
	return out
}

// Check returns a human-readable error listing everything that is missing or misconfigured.
func Check(ctx context.Context, uri, network string) error {
	var probs []string
	if m := MissingTools(exec.LookPath); len(m) > 0 {
		probs = append(probs, "missing tools: "+strings.Join(m, ", "))
	}
	if len(probs) == 0 {
		if out, err := exec.CommandContext(ctx, "virsh", "-c", uri, "list").CombinedOutput(); err != nil {
			probs = append(probs, fmt.Sprintf("cannot connect to libvirt (%s): %s\n    hint: apt install libvirt-daemon-system qemu-system-x86; systemctl enable --now libvirtd; add your user to the 'libvirt' group",
				uri, strings.TrimSpace(string(out))))
		} else if out, err := exec.CommandContext(ctx, "virsh", "-c", uri, "net-info", network).CombinedOutput(); err != nil {
			probs = append(probs, fmt.Sprintf("libvirt network %q not available: %s\n    hint: virsh net-start %s; virsh net-autostart %s",
				network, strings.TrimSpace(string(out)), network, network))
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
