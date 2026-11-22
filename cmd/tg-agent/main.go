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
		Long: `Creates the VM on first run (or after "tg-agent delete"), starts it and waits
until it is provisioned and Takopi runs.

When it creates the VM, it asks for its CPUs, memory and disk size, offering
the config's values, and saves the answers to the config; without a terminal it
uses the config's values. To change them later, use "tg-agent update".`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error { return cmd.Start(c.Context()) },
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

	var res cmd.ResourceFlags
	update := &cobra.Command{
		Use:   "update [--cpus N] [--memory SIZE] [--disk SIZE]",
		Short: "Apply the config to the VM, re-run provisioning (install and upgrade tools), restart Takopi",
		Long: `Writes the given resource flags to the config, then brings the running VM in
line with the config: grows its disk, sets its CPUs and memory, pushes the
Telegram settings into Takopi's config, re-runs the provisioning (apt upgrade,
installs new tools, upgrades Claude, Takopi etc.) and restarts Takopi.

Values changed by hand in the config are applied as well, so "tg-agent update"
without flags reconciles the VM with the config.

  --cpus N       number of CPUs, at most the host's
  --memory SIZE  memory, e.g. 16G or 16384M; a plain number means GiB
  --disk SIZE    disk size, e.g. 100G; a plain number means GiB. The disk
                 only grows, inside the VM at once.

New CPUs and memory take effect when the VM restarts. update shows the old and
new values and asks whether to restart the VM now (running sessions are
interrupted); without a terminal it refuses, and the change takes effect on the
next start of the VM.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error { return cmd.Update(c.Context(), res) },
	}
	update.Flags().IntVar(&res.CPUs, "cpus", 0, "number of CPUs")
	update.Flags().StringVar(&res.Memory, "memory", "", "memory, e.g. 16G (plain number: GiB)")
	update.Flags().StringVar(&res.Disk, "disk", "", "disk size, e.g. 100G (plain number: GiB); can only grow")

	shell := &cobra.Command{
		Use:     "shell [<command> [<args>...]]",
		Aliases: []string{"sh", "ssh"},
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
