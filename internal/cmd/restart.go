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
	fmt.Println("Shutting the VM down ...")
	if err := m.Stop(ctx); err != nil {
		return err
	}
	fmt.Println("VM stopped, starting it again ...")
	if err := boot(ctx, m); err != nil {
		return err
	}
	fmt.Println("VM restarted.")
	return nil
}
