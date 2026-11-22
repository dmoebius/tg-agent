package cmd

import (
	"context"
	"fmt"

	"github.com/dmoebius/tg-agent/internal/vm"
)

// Restart shuts the running VM down and starts it again, like stop followed
// by start. It fails if the VM is not running.
func Restart(ctx context.Context) error {
	m, err := setup(ctx)
	if err != nil {
		return err
	}
	st, err := m.State(ctx)
	if err != nil {
		return err
	}
	if st != vm.Running {
		return fmt.Errorf("VM is %s, not running; use `tg-agent start`", st)
	}
	// Check the bot's rights before shutting down, so a problem there does
	// not leave the VM stopped.
	forum, err := checkForum(ctx, m.Cfg.Telegram)
	if err != nil {
		return err
	}
	fmt.Println("Shutting the VM down ...")
	if err := m.Stop(ctx); err != nil {
		return err
	}
	fmt.Println("VM stopped, starting it again ...")
	return boot(ctx, m, forum)
}
