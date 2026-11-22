package cmd

import (
	"context"
	"fmt"

	"github.com/dmoebius/tg-agent/internal/vm"
)

// Update pushes the Telegram settings into Takopi's config, re-runs the
// provisioning and restarts Takopi. The VM must be running.
func Update(ctx context.Context) error {
	m, err := setup(ctx)
	if err != nil {
		return err
	}
	if st, err := m.State(ctx); err != nil || st != vm.Running {
		return fmt.Errorf("VM must be running (state=%v, err=%v); run `tg-agent start`", st, err)
	}
	if _, err := checkForum(ctx, m.Cfg.Telegram); err != nil {
		return err
	}
	c, err := m.Connect(ctx, 0)
	if err != nil {
		return err
	}
	return c.Update(ctx, m.Cfg.Telegram)
}
