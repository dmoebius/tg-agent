package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/dmoebius/tg-agent/internal/config"
	"github.com/dmoebius/tg-agent/internal/prompt"
	"github.com/dmoebius/tg-agent/internal/vm"
)

// ResourceFlags are update's flags for the VM's resources. Zero values are
// not set and keep the Config's value.
type ResourceFlags struct {
	CPUs   int
	Memory string // e.g. "16G"; a plain number means MiB
	Disk   string // e.g. "100G"; a plain number means GiB
}

// apply returns v with the flags set in f.
func (f ResourceFlags) apply(v config.VM) (config.VM, error) {
	var err error
	if f.CPUs != 0 {
		v.CPUs = f.CPUs
	}
	if f.Memory != "" {
		if v.MemoryMB, err = config.ParseMemory(f.Memory); err != nil {
			return v, fmt.Errorf("--memory: %w", err)
		}
	}
	if f.Disk != "" {
		if v.DiskGB, err = config.ParseDisk(f.Disk); err != nil {
			return v, fmt.Errorf("--disk: %w", err)
		}
	}
	return v, nil
}

// checkResources validates v's resources against the Host and prints
// warnings. minDiskGB is the size of an existing disk (0 if none).
func checkResources(v config.VM, minDiskGB int) error {
	warnings, err := v.CheckResources(config.ThisHost(), minDiskGB)
	if err != nil {
		return fmt.Errorf("VM resources: %w", err)
	}
	for _, w := range warnings {
		fmt.Println("warning:", w)
	}
	return nil
}

// chooseResources settles the resources of a VM about to be created:
// interactively it asks for them, offering the Config's values, and saves the
// answers to the Config; without a terminal it takes the Config's values.
func chooseResources(ctx context.Context, m *vm.Manager) error {
	existing, err := m.DiskImageGB(ctx)
	if err != nil {
		return err
	}
	if !prompt.Interactive() {
		return checkResources(m.Cfg.VM, existing)
	}
	h := config.ThisHost()
	fmt.Printf("Resources of the VM (the host has %d CPUs and %s of memory", h.CPUs, config.FormatMB(h.MemoryMB))
	if existing > 0 {
		fmt.Printf("; the existing disk has %d GiB and can only grow", existing)
	}
	fmt.Println("):")
	v := m.Cfg.VM
	for {
		cpus, err := prompt.Int64("  CPUs", int64(v.CPUs))
		if err != nil {
			return err
		}
		v.CPUs = int(cpus)
		if v.MemoryMB, err = askSize("  Memory (e.g. 16G)", config.FormatMB(v.MemoryMB), config.ParseMemory); err != nil {
			return err
		}
		if v.DiskGB, err = askSize("  Disk (e.g. 100G)", fmt.Sprintf("%d GiB", max(v.DiskGB, existing)), config.ParseDisk); err != nil {
			return err
		}
		err = checkResources(v, existing)
		if err == nil {
			break
		}
		fmt.Println(" ", err)
	}
	if v == m.Cfg.VM {
		return nil
	}
	m.Cfg.VM = v
	if err := m.Cfg.Save(); err != nil {
		return err
	}
	fmt.Println("Saved", config.Path())
	return nil
}

// askSize asks until parse accepts the answer.
func askSize(label, def string, parse func(string) (int, error)) (int, error) {
	for {
		s, err := prompt.String(label, def)
		if err != nil {
			return 0, err
		}
		n, err := parse(s)
		if err == nil {
			return n, nil
		}
		fmt.Println(" ", err)
	}
}

// restartForResources restarts the VM, after confirmation, so the CPUs and
// memory set for its next boot take effect. Without a terminal it refuses,
// because a restart interrupts running Sessions.
func restartForResources(ctx context.Context, m *vm.Manager, current, want vm.Resources) error {
	fmt.Println("\nThe new CPUs and memory take effect when the VM restarts:")
	if current.CPUs != want.CPUs {
		fmt.Printf("  CPUs:   %d -> %d\n", current.CPUs, want.CPUs)
	}
	if current.MemoryMB != want.MemoryMB {
		fmt.Printf("  Memory: %s -> %s\n", config.FormatMB(current.MemoryMB), config.FormatMB(want.MemoryMB))
	}
	const later = "`tg-agent stop && tg-agent start`"
	if !prompt.Interactive() {
		return fmt.Errorf("a restart interrupts running sessions and needs confirmation in a terminal; restart the VM with %s or run `tg-agent update` in a terminal", later)
	}
	ok, err := prompt.Confirm("Restart the VM now? Running sessions are interrupted")
	if err != nil {
		return err
	}
	if !ok {
		fmt.Printf("Not restarted. The change takes effect on the next start of the VM: %s\n", later)
		return nil
	}
	fmt.Println("Shutting the VM down ...")
	if err := m.Stop(ctx); err != nil {
		return err
	}
	fmt.Println("Starting the VM ...")
	if err := m.EnsureRunning(ctx); err != nil {
		return err
	}
	if _, err := m.Connect(ctx, 5*time.Minute); err != nil {
		return err
	}
	fmt.Println("VM restarted.")
	return nil
}

// describeResource formats a resource for status: its current value, the
// value pending a restart and the Config's value, if they differ.
func describeResource(current, next, cfg, hint string) string {
	s := current
	if next != current {
		s += fmt.Sprintf(" (%s pending restart)", next)
	}
	if cfg != next {
		s += fmt.Sprintf(" (config: %s, %s)", cfg, hint)
	}
	return s
}
