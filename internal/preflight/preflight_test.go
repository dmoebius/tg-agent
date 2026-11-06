package preflight

import (
	"errors"
	"strings"
	"testing"
)

func TestMissingTools(t *testing.T) {
	look := func(b string) (string, error) {
		if b == "virt-install" || b == "qemu-img" {
			return "", errors.New("nf")
		}
		return "/usr/bin/" + b, nil
	}
	m := strings.Join(MissingTools(look), ",")
	if !strings.Contains(m, "virtinst") || !strings.Contains(m, "qemu-utils") || strings.Contains(m, "virsh") {
		t.Fatal(m)
	}
}
