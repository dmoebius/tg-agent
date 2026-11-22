package cmd

import (
	"testing"

	"github.com/dmoebius/tg-agent/internal/config"
)

func TestResourceFlagsApply(t *testing.T) {
	v := config.VM{CPUs: 4, MemoryMB: 8192, DiskGB: 60, Network: "default"}
	got, err := ResourceFlags{}.apply(v)
	if err != nil || got != v {
		t.Fatalf("no flags changed the Config: %+v %v", got, err)
	}
	got, err = ResourceFlags{CPUs: 6, Memory: "16G", Disk: "100"}.apply(v)
	want := config.VM{CPUs: 6, MemoryMB: 16384, DiskGB: 100, Network: "default"}
	if err != nil || got != want {
		t.Fatalf("got %+v %v, want %+v", got, err, want)
	}
	if _, err := (ResourceFlags{Memory: "lots"}).apply(v); err == nil {
		t.Fatal("invalid --memory: expected error")
	}
}

func TestDescribeResource(t *testing.T) {
	for _, tc := range []struct{ current, next, cfg, want string }{
		{"4", "4", "4", "4"},
		{"4", "8", "8", "4 (8 pending restart)"},
		{"4", "4", "8", "4 (Config: 8, hint)"},
		{"4", "6", "8", "4 (6 pending restart) (Config: 8, hint)"},
	} {
		if got := describeResource(tc.current, tc.next, tc.cfg, "hint"); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
}
