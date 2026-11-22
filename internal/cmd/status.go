package cmd

import (
	"context"
	"fmt"

	"github.com/dmoebius/tg-agent/internal/config"
	"github.com/dmoebius/tg-agent/internal/telegram"
	"github.com/dmoebius/tg-agent/internal/vm"
)

// Status prints the config paths, the Forum link, the VM state and resources
// and, if the VM runs, its IP, the Takopi service state and the SSH command.
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
	if st != vm.Missing {
		printResources(ctx, m, field)
	}
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

// printResources prints the VM's CPUs, memory and disk size, flagging values
// that wait for a restart or differ from the Config.
func printResources(ctx context.Context, m *vm.Manager, field func(string, any)) {
	current, next, err := m.Resources(ctx)
	if err != nil {
		field("Resources", "unknown: "+err.Error())
		return
	}
	want := vm.Wanted(m.Cfg.VM)
	const update = "run `tg-agent update`"
	field("CPUs", describeResource(fmt.Sprint(current.CPUs), fmt.Sprint(next.CPUs), fmt.Sprint(want.CPUs), update))
	field("Memory", describeResource(config.FormatMB(current.MemoryMB), config.FormatMB(next.MemoryMB), config.FormatMB(want.MemoryMB), update))
	disk := func(gb int) string { return fmt.Sprintf("%d GiB", gb) }
	hint := update
	if want.DiskGB < next.DiskGB {
		hint = "the disk cannot shrink"
	}
	field("Disk", describeResource(disk(current.DiskGB), disk(next.DiskGB), disk(want.DiskGB), hint))
}
