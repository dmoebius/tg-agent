package config

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// Limits for the VM's resources.
const (
	MinMemoryMB = 2048
	// MinDiskGB leaves room for the image (about 4 GiB) plus the tooling
	// Provisioning installs.
	MinDiskGB = 20
)

// DefaultCPUs is half of the CPUs available to tg-agent on the Host (what
// nproc reports, honouring CPU affinity), at least 1.
func DefaultCPUs() int { return max(1, runtime.NumCPU()/2) }

// Host describes the Host's capacity, to validate the VM's resources against.
type Host struct {
	CPUs     int
	MemoryMB int // 0 if unknown
}

// ThisHost returns the capacity of the Host tg-agent runs on.
func ThisHost() Host {
	return Host{CPUs: runtime.NumCPU(), MemoryMB: hostMemoryMB()}
}

func hostMemoryMB() int {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		// MemTotal:        8126460 kB
		if fs := strings.Fields(s.Text()); len(fs) >= 2 && fs[0] == "MemTotal:" {
			kb, _ := strconv.Atoi(fs[1])
			return kb / 1024
		}
	}
	return 0
}

// CheckResources validates the VM's CPUs, memory and disk size against the
// Host. The disk must be at least minDiskGB (e.g. the size of the existing
// disk, which cannot shrink). Warnings are returned for values that work but
// are risky.
func (v VM) CheckResources(h Host, minDiskGB int) (warnings []string, err error) {
	if v.CPUs < 1 || v.CPUs > h.CPUs {
		return nil, fmt.Errorf("cpus = %d: must be between 1 and the host's %d CPUs", v.CPUs, h.CPUs)
	}
	if v.MemoryMB < MinMemoryMB {
		return nil, fmt.Errorf("memory %s: must be at least %s", FormatMB(v.MemoryMB), FormatMB(MinMemoryMB))
	}
	if h.MemoryMB > 0 && v.MemoryMB > h.MemoryMB {
		return nil, fmt.Errorf("memory %s: the host has only %s", FormatMB(v.MemoryMB), FormatMB(h.MemoryMB))
	}
	if h.MemoryMB > 0 && v.MemoryMB > h.MemoryMB*3/4 {
		warnings = append(warnings, fmt.Sprintf("memory %s is more than 75%% of the host's %s; the host may start swapping",
			FormatMB(v.MemoryMB), FormatMB(h.MemoryMB)))
	}
	minDisk := max(minDiskGB, MinDiskGB)
	if v.DiskGB < minDisk {
		if minDiskGB > MinDiskGB {
			return nil, fmt.Errorf("disk %d GiB: the VM's disk already has %d GiB and cannot shrink", v.DiskGB, minDiskGB)
		}
		return nil, fmt.Errorf("disk %d GiB: must be at least %d GiB", v.DiskGB, MinDiskGB)
	}
	return warnings, nil
}

var sizeRe = regexp.MustCompile(`(?i)^\s*(\d+(?:\.\d+)?)\s*(?:([mgt])(?:i?b)?)?\s*$`)

// parseMB parses a size like "16G", "16384M", "1.5GiB" or "2T" into MiB;
// units are binary. A plain number is taken in units of defMB MiB.
func parseMB(s string, defMB float64) (float64, error) {
	mm := sizeRe.FindStringSubmatch(s)
	if mm == nil {
		return 0, fmt.Errorf("invalid size %q (examples: 16G, 16384M)", s)
	}
	n, err := strconv.ParseFloat(mm[1], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	unit := defMB
	switch strings.ToLower(mm[2]) {
	case "m":
		unit = 1
	case "g":
		unit = 1024
	case "t":
		unit = 1024 * 1024
	}
	return n * unit, nil
}

// ParseMemory parses a memory size like "16G" or "16384M" into MiB. A plain
// number means MiB, like memory_mb in the Config.
func ParseMemory(s string) (int, error) {
	mb, err := parseMB(s, 1)
	if err != nil {
		return 0, err
	}
	if mb != math.Trunc(mb) || mb > math.MaxInt32 {
		return 0, fmt.Errorf("invalid memory size %q: must be a whole number of MiB", s)
	}
	return int(mb), nil
}

// ParseDisk parses a disk size like "100G" into GiB. A plain number means
// GiB, like disk_gb in the Config.
func ParseDisk(s string) (int, error) {
	mb, err := parseMB(s, 1024)
	if err != nil {
		return 0, err
	}
	gb := mb / 1024
	if gb != math.Trunc(gb) || gb > math.MaxInt32 {
		return 0, fmt.Errorf("invalid disk size %q: must be a whole number of GiB", s)
	}
	return int(gb), nil
}

// FormatMB formats a memory size in MiB, as GiB if it is a whole number of
// them: "8 GiB", "1536 MiB".
func FormatMB(mb int) string {
	if mb != 0 && mb%1024 == 0 {
		return fmt.Sprintf("%d GiB", mb/1024)
	}
	return fmt.Sprintf("%d MiB", mb)
}
