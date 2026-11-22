package cmd

import (
	"context"
	"fmt"

	"github.com/dmoebius/tg-agent/internal/vm"
)

// Stop shuts the VM down if it is running.
func Stop(ctx context.Context) error {
	m, err := setup(ctx)
	if err != nil {
		return err
	}
	st, err := m.State(ctx)
	if err != nil {
		return err
	}
	if st != vm.Running {
		fmt.Println("VM is", st)
		return nil
	}
	if err := m.Stop(ctx); err != nil {
		return err
	}
	fmt.Println("VM stopped.")
	return nil
}
