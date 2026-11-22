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
	err := newRootCmd().ExecuteContext(ctx)
	code, show := exitCode(err)
	if show {
		fmt.Fprintln(os.Stderr, "error:", err)
	}
	if code != 0 {
		stop()
		os.Exit(code)
	}
}

// exitCode returns the exit status for the error of a command and whether
// the error should be printed. The exit status of a command run in the VM is
// passed through silently, Ctrl-C at a prompt exits like a shell.
func exitCode(err error) (code int, show bool) {
	var exit *vm.ExitError
	switch {
	case err == nil:
		return 0, false
	case errors.As(err, &exit):
		return exit.Code, false
	case errors.Is(err, prompt.ErrInterrupted):
		return 130, false
	default:
		return 1, true
	}
}

// newRootCmd defines all commands with their flags.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "tg-agent",
		Short:         "Run Claude in a libvirt VM and bridge sessions to Telegram via Takopi",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	start := &cobra.Command{
		Use:   "start",
		Short: "Create the VM on first run, start it and wait until Takopi runs",
		Args:  cobra.NoArgs,
		RunE:  func(c *cobra.Command, _ []string) error { return cmd.Start(c.Context()) },
	}

	stop := &cobra.Command{
		Use:   "stop",
		Short: "Shut the VM down",
		Args:  cobra.NoArgs,
		RunE:  func(c *cobra.Command, _ []string) error { return cmd.Stop(c.Context()) },
	}

	status := &cobra.Command{
		Use:   "status",
		Short: "Show VM and Takopi status",
		Args:  cobra.NoArgs,
		RunE:  func(c *cobra.Command, _ []string) error { return cmd.Status(c.Context()) },
	}

	update := &cobra.Command{
		Use:   "update",
		Short: "Push Telegram settings, re-run provisioning (install and upgrade tools), restart Takopi",
		Args:  cobra.NoArgs,
		RunE:  func(c *cobra.Command, _ []string) error { return cmd.Update(c.Context()) },
	}

	shell := &cobra.Command{
		Use:     "shell [<command> [<args>...]]",
		Aliases: []string{"sh"},
		Short:   "Open a login shell in the VM, or run a command there",
		Long: `Without arguments, opens an interactive login shell in the VM as user agent.
With arguments, runs <command> <args> in a login shell in the VM and exits with
its exit code. Everything after "shell" belongs to the remote command, flags too.`,
		DisableFlagParsing: true,
		RunE:               func(c *cobra.Command, args []string) error { return cmd.Shell(c.Context(), args) },
	}

	var wipeDisk, wipeAll bool
	del := &cobra.Command{
		Use:   "delete [--wipe-disk | --wipe-all]",
		Short: "Remove the VM, optionally its disk and the config, to start over",
		Long: fmt.Sprintf(`Removes the VM instance: shuts it down (forcing it off after 30 seconds) and
removes it from libvirt (virsh undefine). The disk, data_dir and config are
kept, so a later "tg-agent start" re-creates the VM on the existing disk.

  --wipe-disk  additionally removes data_dir (disk, base image, SSH key,
               cloud-init files) and the libvirt storage pool pointing at it.
               All projects and sessions in the VM are lost.
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
	del.Flags().BoolVar(&wipeDisk, "wipe-disk", false, "also remove data_dir (disk, SSH key, ...) and its storage pool")
	del.Flags().BoolVar(&wipeAll, "wipe-all", false, "also remove data_dir and the whole config directory incl. the bot token (implies --wipe-disk)")

	var fix bool
	doctor := &cobra.Command{
		Use:   "doctor [--fix]",
		Short: "Find out why the bot does not answer, and optionally repair it",
		Long: `Checks the whole path from Telegram to Claude: the bot and its rights in the
forum, the VM, the VM's network (gateway, DNS, internet, Telegram), the Takopi
service, its config and its log, and Claude's login. "status" only shows
whether the Takopi service runs; "doctor" shows whether it can actually work.

Without flags it only reports. With --fix it also repairs what it can: starts
a stopped VM, lets the VM's traffic through Docker's firewall on the host
(asks for your sudo password and
prints every sudo command before running it), removes a webhook and restarts Takopi.

Exits with status 1 if problems remain.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error { return cmd.Doctor(c.Context(), fix) },
	}
	doctor.Flags().BoolVar(&fix, "fix", false, "repair what can be repaired (may ask for your sudo password)")

	root.AddCommand(start, stop, status, update, shell, del, doctor)
	return root
}
