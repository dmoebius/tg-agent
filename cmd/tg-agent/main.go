// Command tg-agent defines the CLI: commands, flags and help texts. The
// commands are implemented in internal/cmd.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/dmoebius/tg-agent/internal/cmd"
	"github.com/dmoebius/tg-agent/internal/config"
	"github.com/dmoebius/tg-agent/internal/prompt"
	"github.com/dmoebius/tg-agent/internal/vm"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := newRootCmd().ExecuteContext(ctx); err != nil {
		var exit *vm.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.Code)
		}
		if errors.Is(err, prompt.ErrInterrupted) {
			os.Exit(130) // like a shell after Ctrl-C
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "tg-agent",
		Short:         "Run Claude in a libvirt VM and bridge sessions to Telegram via Takopi",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		&cobra.Command{
			Use:   "start",
			Short: "Create the VM on first run, start it and wait until Takopi runs",
			Args:  cobra.NoArgs,
			RunE:  func(c *cobra.Command, _ []string) error { return cmd.Start(c.Context()) },
		},
		&cobra.Command{
			Use:   "stop",
			Short: "Shut the VM down",
			Args:  cobra.NoArgs,
			RunE:  func(c *cobra.Command, _ []string) error { return cmd.Stop(c.Context()) },
		},
		&cobra.Command{
			Use:   "status",
			Short: "Show VM and Takopi status",
			Args:  cobra.NoArgs,
			RunE:  func(c *cobra.Command, _ []string) error { return cmd.Status(c.Context()) },
		},
		&cobra.Command{
			Use:   "update",
			Short: "Push Telegram settings, re-run provisioning (install and upgrade tools), restart Takopi",
			Args:  cobra.NoArgs,
			RunE:  func(c *cobra.Command, _ []string) error { return cmd.Update(c.Context()) },
		},
		&cobra.Command{
			Use:     "shell [<command> [<args>...]]",
			Aliases: []string{"sh"},
			Short:   "Open a login shell in the VM, or run a command there",
			Long: `Without arguments, opens an interactive login shell in the VM as user agent.
With arguments, runs <command> <args> in a login shell in the VM and exits with
its exit code. Everything after "shell" belongs to the remote command, flags too.`,
			DisableFlagParsing: true,
			RunE:               func(c *cobra.Command, args []string) error { return cmd.Shell(c.Context(), args) },
		},
		newDeleteCmd(),
	)
	return root
}

func newDeleteCmd() *cobra.Command {
	var wipeDisk, wipeAll bool
	c := &cobra.Command{
		Use:   "delete [--wipe-disk | --wipe-all]",
		Short: "Remove the VM, optionally its disk and the config, to start over",
		Long: fmt.Sprintf(`Removes the VM instance: shuts it down (forcing it off after 30 seconds) and
removes it from libvirt (virsh undefine). The disk, data_dir and config are
kept, so a later "tg-agent start" re-creates the VM on the existing disk.

  --wipe-disk  additionally removes data_dir (disk, base image, SSH key,
               cloud-init files) and the libvirt storage pool pointing at it.
               All Projects and Sessions in the VM are lost.
  --wipe-all   additionally removes the config directory
               %s COMPLETELY,
               including config.toml with the Telegram bot token and ids;
               the next "tg-agent start" behaves like a first run, including
               the Telegram setup.

Asks for confirmation by typing the VM name; refuses to run without a terminal.`,
			config.Dir()),
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return cmd.Delete(c.Context(), wipeDisk || wipeAll, wipeAll)
		},
	}
	c.Flags().BoolVar(&wipeDisk, "wipe-disk", false, "also remove data_dir (disk, SSH key, ...) and its storage pool")
	c.Flags().BoolVar(&wipeAll, "wipe-all", false, "also remove data_dir and the whole config directory incl. the bot token (implies --wipe-disk)")
	return c
}
