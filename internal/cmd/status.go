package cmd

import (
	"context"
	"fmt"

	"github.com/dmoebius/tg-agent/internal/config"
	"github.com/dmoebius/tg-agent/internal/telegram"
	"github.com/dmoebius/tg-agent/internal/vm"
)

// Status prints the config paths, the Forum link, the VM state and, if the VM
// runs, its IP, the Takopi service state and the SSH command.
func Status(ctx context.Context) error {
	m, err := setup(ctx)
	if err != nil {
		return err
	}
	st, err := m.State(ctx)
	if err != nil {
		return err
	}
	field := func(label string, v any) { fmt.Printf("%-10s%v\n", label+":", v) }
	field("Config", config.Path())
	field("data_dir", m.Cfg.VM.DataDir)
	field("Forum", telegram.ForumWebLink(m.Cfg.Telegram.ChatID))
	field("VM", st)
	if st != vm.Running {
		return nil
	}
	c, err := m.Connect(ctx, 0)
	if err != nil {
		field("SSH", "unreachable: "+err.Error())
		return nil
	}
	field("IP", c.Host)
	field("Takopi", c.TakopiStatus(ctx))
	field("SSH", c.SSHCommand())
	return nil
}
