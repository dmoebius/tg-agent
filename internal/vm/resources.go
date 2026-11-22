package vm

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/dmoebius/tg-agent/internal/config"
)

// Resources are the VM's CPUs, memory and disk size.
type Resources struct {
	CPUs     int
	MemoryMB int
	DiskGB   int
}

// Wanted returns the resources the Config asks for.
func Wanted(v config.VM) Resources {
	return Resources{CPUs: v.CPUs, MemoryMB: v.MemoryMB, DiskGB: v.DiskGB}
}

// SameCPUsMemory reports whether r and o have the same CPUs and memory, the
// resources that take effect only on the next boot.
func (r Resources) SameCPUsMemory(o Resources) bool {
	return r.CPUs == o.CPUs && r.MemoryMB == o.MemoryMB
}

// Resources returns the VM's resources as libvirt knows them: current are
// those it has now (if it is shut off, those it boots with), next those it
// boots with next time. The disk size is the same in both.
func (m *Manager) Resources(ctx context.Context) (current, next Resources, err error) {
	x, err := m.virsh(ctx, "dumpxml", "--inactive", config.VMName)
	if err != nil {
		return current, next, err
	}
	if next.CPUs, next.MemoryMB, err = parseDomainResources(x); err != nil {
		return current, next, err
	}
	current = next
	if ds, err := m.domstate(ctx); err == nil && ds != "shut off" {
		x, err := m.virsh(ctx, "dumpxml", config.VMName)
		if err != nil {
			return current, next, err
		}
		if current.CPUs, current.MemoryMB, err = parseDomainResources(x); err != nil {
			return current, next, err
		}
	}
	out, err := m.virsh(ctx, "domblkinfo", config.VMName, m.disk())
	if err != nil {
		return current, next, err
	}
	b, err := parseCapacity(out)
	if err != nil {
		return current, next, err
	}
	current.DiskGB = int(b >> 30)
	next.DiskGB = current.DiskGB
	return current, next, nil
}

// parseDomainResources returns the vCPUs and the memory in MiB of a domain
// XML. A guest sees <memory> as its RAM; the vCPUs it runs with are <vcpu
// current=...>, or all of them if current is not set.
func parseDomainResources(domXML string) (cpus, memoryMB int, err error) {
	var d struct {
		VCPU struct {
			Max     int `xml:",chardata"`
			Current int `xml:"current,attr"`
		} `xml:"vcpu"`
		Memory struct {
			Value int64  `xml:",chardata"`
			Unit  string `xml:"unit,attr"`
		} `xml:"memory"`
	}
	if err := xml.Unmarshal([]byte(domXML), &d); err != nil {
		return 0, 0, fmt.Errorf("parse domain XML: %w", err)
	}
	cpus = d.VCPU.Max
	if d.VCPU.Current > 0 {
		cpus = d.VCPU.Current
	}
	kib := map[string]int64{"": 1, "KiB": 1, "k": 1, "MiB": 1 << 10, "M": 1 << 10, "GiB": 1 << 20, "G": 1 << 20}
	f, ok := kib[d.Memory.Unit]
	if !ok {
		return 0, 0, fmt.Errorf("parse domain XML: unexpected memory unit %q", d.Memory.Unit)
	}
	if cpus == 0 || d.Memory.Value == 0 {
		return 0, 0, errors.New("parse domain XML: no vcpu or memory")
	}
	return cpus, int(d.Memory.Value * f / 1024), nil
}

// parseCapacity returns the capacity in bytes from `virsh domblkinfo`.
func parseCapacity(out string) (int64, error) {
	for _, l := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(l, ":"); ok && strings.TrimSpace(k) == "Capacity" {
			return strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		}
	}
	return 0, fmt.Errorf("no capacity in domblkinfo output: %q", out)
}

// SetCPUsMemory sets the VM's CPUs and memory to want, effective on its
// next boot. next are the values the VM currently boots with.
func (m *Manager) SetCPUsMemory(ctx context.Context, next, want Resources) error {
	var cmds [][]string
	if want.CPUs != next.CPUs {
		// The count must never exceed the maximum, so raise the maximum
		// first and lower it last.
		cnt := []string{"setvcpus", config.VMName, fmt.Sprint(want.CPUs), "--config"}
		maxi := append(append([]string{}, cnt...), "--maximum")
		if want.CPUs > next.CPUs {
			cmds = append(cmds, maxi, cnt)
		} else {
			cmds = append(cmds, cnt, maxi)
		}
	}
	if want.MemoryMB != next.MemoryMB {
		kib := fmt.Sprint(want.MemoryMB * 1024)
		mem := []string{"setmem", config.VMName, kib, "--config"}
		maxi := []string{"setmaxmem", config.VMName, kib, "--config"}
		if want.MemoryMB > next.MemoryMB {
			cmds = append(cmds, maxi, mem)
		} else {
			cmds = append(cmds, mem, maxi)
		}
	}
	for _, c := range cmds {
		if _, err := m.virsh(ctx, c...); err != nil {
			return err
		}
	}
	return nil
}

// GrowDisk grows the VM's disk to gb GiB: online if the VM runs, otherwise
// the image file. The partition and filesystem inside the VM grow on its
// next boot (cloud-init's growpart and resizefs run on every boot), or at
// once with Conn.GrowRootFS.
func (m *Manager) GrowDisk(ctx context.Context, gb int) error {
	size := fmt.Sprintf("%dG", gb)
	if st, err := m.State(ctx); err != nil {
		return err
	} else if st == Running {
		_, err := m.virsh(ctx, "blockresize", config.VMName, m.disk(), size)
		return err
	}
	_, err := m.run(ctx, "qemu-img", "resize", m.disk(), size)
	return err
}

// DiskImageGB returns the size of the disk image file in GiB, or 0 if it
// does not exist. Use it while no VM is defined; see Resources otherwise.
func (m *Manager) DiskImageGB(ctx context.Context) (int, error) {
	if _, err := os.Stat(m.disk()); errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	out, err := m.run(ctx, "qemu-img", "info", "--output=json", "-U", m.disk())
	if err != nil {
		return 0, err
	}
	var info struct {
		VirtualSize int64 `json:"virtual-size"`
	}
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		return 0, fmt.Errorf("qemu-img info %s: %w", m.disk(), err)
	}
	return int(info.VirtualSize >> 30), nil
}
