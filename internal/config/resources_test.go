package config

import (
	"strings"
	"testing"
)

func TestParseMemory(t *testing.T) {
	for in, want := range map[string]int{
		"8":      8192,
		"1.5":    1536,
		"16G":    16384,
		"16g":    16384,
		"16GiB":  16384,
		"16 GB":  16384,
		"16384M": 16384,
		"1.5G":   1536,
		"1T":     1024 * 1024,
	} {
		if got, err := ParseMemory(in); err != nil || got != want {
			t.Errorf("ParseMemory(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "G", "-1G", "16K", "16X", "0.3M", "16 G B"} {
		if got, err := ParseMemory(in); err == nil {
			t.Errorf("ParseMemory(%q) = %d, want error", in, got)
		}
	}
}

func TestParseDisk(t *testing.T) {
	for in, want := range map[string]int{
		"100":     100,
		"100G":    100,
		"102400M": 100,
		"1T":      1024,
		"1.5T":    1536,
	} {
		if got, err := ParseDisk(in); err != nil || got != want {
			t.Errorf("ParseDisk(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"100.5G", "1000M", "abc"} {
		if got, err := ParseDisk(in); err == nil {
			t.Errorf("ParseDisk(%q) = %d, want error", in, got)
		}
	}
}

func TestCheckResources(t *testing.T) {
	h := Host{CPUs: 8, MemoryMB: 16384}
	ok := VM{CPUs: 4, MemoryMB: 8192, DiskGB: 60}
	if w, err := ok.CheckResources(h, 0); err != nil || len(w) > 0 {
		t.Fatalf("valid resources: %v %v", w, err)
	}
	for name, tc := range map[string]struct {
		mutate  func(*VM)
		minDisk int
		want    string
	}{
		"no cpus":           {func(v *VM) { v.CPUs = 0 }, 0, "between 1 and"},
		"too many cpus":     {func(v *VM) { v.CPUs = 9 }, 0, "between 1 and"},
		"too little memory": {func(v *VM) { v.MemoryMB = 1024 }, 0, "at least 2 GiB"},
		"more than host":    {func(v *VM) { v.MemoryMB = 32768 }, 0, "only 16 GiB"},
		"tiny disk":         {func(v *VM) { v.DiskGB = 5 }, 0, "at least 20 GiB"},
		"shrink":            {func(v *VM) { v.DiskGB = 50 }, 60, "cannot shrink"},
	} {
		v := ok
		tc.mutate(&v)
		if _, err := v.CheckResources(h, tc.minDisk); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want error containing %q", name, err, tc.want)
		}
	}
	big := ok
	big.MemoryMB = 14336
	if w, err := big.CheckResources(h, 0); err != nil || len(w) != 1 {
		t.Errorf("expected a warning above 75%% of the Host's memory: %v %v", w, err)
	}
}

func TestFormatMB(t *testing.T) {
	if got := FormatMB(8192); got != "8 GiB" {
		t.Error(got)
	}
	if got := FormatMB(1536); got != "1536 MiB" {
		t.Error(got)
	}
}
