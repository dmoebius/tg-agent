package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/dmoebius/tg-agent/internal/prompt"
	"github.com/dmoebius/tg-agent/internal/vm"
)

// Start creates the VM on first run (asking for its resources), starts it
// and waits until it is provisioned and Claude is logged in.
func Start(ctx context.Context) error {
	m, err := setup(ctx)
	if err != nil {
		return err
	}
	st, err := m.State(ctx)
	if err != nil {
		return err
	}
	if st == vm.Missing {
		if err := chooseResources(ctx, m); err != nil {
			return err
		}
	}
	fmt.Println("VM starting...")
	forum, err := checkForum(ctx, m.Cfg.Telegram)
	if err != nil {
		return err
	}
	if err := boot(ctx, m); err != nil {
		return err
	}
	const topic = "General"
	fmt.Printf(`
VM is ready. Telegram forum %[1]q, topic %[2]q is connected to Claude inside the VM.
Ask Claude the first question in %[2]q.
To register a project ask Claude to checkout the repo, then execute `+"`! cd ~/work/<repo> && takopi init <alias>`"+`,
then open a topic for it with `+"`/topic <alias> @<branch>`"+`.
Use `+"`tg-agent status`"+` to get further connection infos.
`, forum, topic)
	return nil
}

// boot creates or starts the VM, waits until it is provisioned and logs
// Claude in if needed.
func boot(ctx context.Context, m *vm.Manager) error {
	if err := m.EnsureRunning(ctx); err != nil {
		return err
	}
	c, err := m.Connect(ctx, 5*time.Minute)
	if err != nil {
		return err
	}
	fmt.Println("Waiting for provisioning (cloud-init) ...")
	if err := c.WaitProvisioned(ctx); err != nil {
		return err
	}
	return c.EnsureClaudeAuth(ctx, prompt.Interactive())
}
