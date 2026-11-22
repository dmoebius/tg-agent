package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/dmoebius/tg-agent/internal/prompt"
)

// Start creates the VM on first run, starts it and waits until it is
// provisioned and Claude is logged in.
func Start(ctx context.Context) error {
	m, err := setup(ctx)
	if err != nil {
		return err
	}
	fmt.Println("VM starting...")
	forum, err := checkForum(ctx, m.Cfg.Telegram)
	if err != nil {
		return err
	}
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
	if err := c.EnsureClaudeAuth(ctx, prompt.Interactive()); err != nil {
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
