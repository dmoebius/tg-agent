package cmd

import (
	"context"
	"fmt"

	"github.com/dmoebius/tg-agent/internal/prompt"
	"github.com/dmoebius/tg-agent/internal/vm"
)

// Shell opens a login shell in the VM, or runs args there if given. A
// non-zero remote exit code is returned as *vm.ExitError.
func Shell(ctx context.Context, args []string) error {
	m, err := setup(ctx)
	if err != nil {
		return err
	}
	if st, err := m.State(ctx); err != nil {
		return err
	} else if st != vm.Running {
		return fmt.Errorf("VM is %s; run `tg-agent start` first", st)
	}
	c, err := m.Connect(ctx, 0)
	if err != nil {
		return err
	}
	return c.Shell(ctx, prompt.Interactive(), args)
}
