package vm

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestParseIP(t *testing.T) {
	out := ` Name       MAC address          Protocol     Address
-------------------------------------------------------------------------------
 vnet0      52:54:00:aa:bb:cc    ipv4         192.168.122.57/24
`
	ip, err := parseIP(out)
	if err != nil || ip != "192.168.122.57" {
		t.Fatalf("got %q, %v", ip, err)
	}
	if _, err := parseIP(" Name MAC\n----\n"); err == nil {
		t.Fatal("expected error")
	}
}

func TestHasDomain(t *testing.T) {
	if !hasDomain("other\ntg-agent\n\n", "tg-agent") {
		t.Error("not found")
	}
	if hasDomain("tg-agent-old\n\n", "tg-agent") || hasDomain("", "tg-agent") {
		t.Error("false positive")
	}
}

func TestDirsToGrant(t *testing.T) {
	root := t.TempDir()
	open := filepath.Join(root, "open")
	closed := filepath.Join(open, "closed")
	if err := os.MkdirAll(closed, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(open, 0o755); err != nil {
		t.Fatal(err)
	}
	got := dirsToGrant(closed, os.Getuid())
	if !slices.Contains(got, closed) || slices.Contains(got, open) {
		t.Fatalf("got %q", got)
	}
	if got := dirsToGrant(closed, os.Getuid()+1); slices.Contains(got, closed) {
		t.Fatalf("granted on a directory of another user: %q", got)
	}
}

func TestKnowsOSVariant(t *testing.T) {
	list := "ubuntu-lts-latest, ubuntu-stable-latest, ubuntu26.04, ubunturesolute\nubuntu24.04, ubuntunoble\n"
	if !knowsOSVariant(list, "ubuntu26.04") || !knowsOSVariant(list, "ubuntu24.04") {
		t.Error("variant not found")
	}
	if knowsOSVariant(list, "ubuntu26") || knowsOSVariant(list, "ubuntu25.10") {
		t.Error("false positive")
	}
}
