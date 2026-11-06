package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "tg-agent",
		Short: "Run Claude in libvirt VMs and bridge sessions to Telegram via Takopi",
	}
	root.AddCommand(newCmd("create <name>", "Create a VM", cobra.ExactArgs(1)))
	root.AddCommand(newCmd("start <name>", "Start Claude + Takopi in the VM", cobra.ExactArgs(1)))
	root.AddCommand(newCmd("stop <name>", "Stop the VM", cobra.ExactArgs(1)))
	root.AddCommand(newCmd("destroy <name>", "Delete the VM", cobra.ExactArgs(1)))
	root.AddCommand(newCmd("list", "List VMs", cobra.NoArgs))
	return root
}

func newCmd(use, short string, args cobra.PositionalArgs) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  args,
		RunE: func(c *cobra.Command, a []string) error {
			return fmt.Errorf("%s: not implemented", c.Name())
		},
	}
}
