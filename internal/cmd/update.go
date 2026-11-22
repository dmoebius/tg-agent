package cmd

import (
	"context"
	"fmt"

	"github.com/dmoebius/tg-agent/internal/config"
	"github.com/dmoebius/tg-agent/internal/vm"
)

// Update writes the resource flags f to the Config, then brings the VM in
// line with the Config: it grows the disk, sets CPUs and memory, pushes the
// Telegram settings into Takopi's config, re-runs the provisioning, restarts
// Takopi and, after confirmation, restarts the VM if CPUs or memory changed.
// The VM must be running.
func Update(ctx context.Context, f ResourceFlags) error {
	m, err := setup(ctx)
	if err != nil {
		return err
	}
	if st, err := m.State(ctx); err != nil || st != vm.Running {
		return fmt.Errorf("VM must be running (state=%v, err=%v); run `tg-agent start`", st, err)
	}
	current, next, err := m.Resources(ctx)
	if err != nil {
		return err
	}
	v, err := f.apply(m.Cfg.VM)
	if err != nil {
		return err
	}
	// Check before saving, so a bad flag does not end up in the Config.
	if err := checkResources(v, next.DiskGB); err != nil {
		return err
	}
	if v != m.Cfg.VM {
		m.Cfg.VM = v
		if err := m.Cfg.Save(); err != nil {
			return err
		}
		fmt.Println("Saved", config.Path())
	}
	if _, err := checkForum(ctx, m.Cfg.Telegram); err != nil {
		return err
	}
	c, err := m.Connect(ctx, 0)
	if err != nil {
		return err
	}
	want := vm.Wanted(v)
	if want.DiskGB > next.DiskGB {
		fmt.Printf("==> grow disk from %d to %d GiB\n", next.DiskGB, want.DiskGB)
		if err := m.GrowDisk(ctx, want.DiskGB); err != nil {
			return err
		}
		if err := c.GrowRootFS(ctx); err != nil {
			return err
		}
	}
	if !want.SameCPUsMemory(next) {
		fmt.Printf("==> set CPUs to %d and memory to %s for the next boot\n", want.CPUs, config.FormatMB(want.MemoryMB))
		if err := m.SetCPUsMemory(ctx, next, want); err != nil {
			return err
		}
	}
	if err := c.Update(ctx, m.Cfg.Telegram); err != nil {
		return err
	}
	if !want.SameCPUsMemory(current) {
		return restartForResources(ctx, m, current, want)
	}
	return nil
}
