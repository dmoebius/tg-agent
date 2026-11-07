package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmoebius/tg-agent/internal/config"
	"github.com/dmoebius/tg-agent/internal/prompt"
	"github.com/dmoebius/tg-agent/internal/vm"
)

func newDeleteCmd() *cobra.Command {
	var wipeDisk, wipeAll bool
	cmd := &cobra.Command{
		Use:   "delete [--wipe-disk | --wipe-all]",
		Short: "Remove the VM, optionally its disk and the config, to start over",
		Long: `Removes the VM instance: shuts it down (forcing it off after 30 seconds) and
removes it from libvirt (virsh undefine). The disk, data_dir and config are
kept, so a later
"tg-agent start" re-creates the VM on the existing disk.

  --wipe-disk  additionally removes data_dir (disk, base image, SSH key,
               cloud-init files) and the libvirt storage pool pointing at it.
               All Projects and Sessions in the VM are lost.
  --wipe-all   additionally removes the config; the next "tg-agent start"
               behaves like a first run, including the Telegram setup.

Asks for confirmation by typing the VM name; refuses to run without a terminal.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDelete(cmd, wipeDisk || wipeAll, wipeAll)
		},
	}
	cmd.Flags().BoolVar(&wipeDisk, "wipe-disk", false, "also remove data_dir (disk, SSH key, ...) and its storage pool")
	cmd.Flags().BoolVar(&wipeAll, "wipe-all", false, "also remove data_dir and the config (implies --wipe-disk)")
	return cmd
}

func runDelete(cmd *cobra.Command, wipeDisk, wipeConfig bool) error {
	ctx := cmd.Context()
	if !prompt.Interactive() {
		return errors.New("delete needs a terminal to confirm; refusing to run non-interactively")
	}
	// No setup(): deleting must work without a complete config and must
	// not start the Telegram setup.
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	m := &vm.Manager{Cfg: cfg, Out: os.Stdout}
	if wipeDisk {
		if err := vm.CheckDataDir(cfg.VM.DataDir); err != nil {
			return err
		}
	}
	inv, err := m.Inventory(ctx)
	if err != nil {
		return err
	}
	cfgExists := false
	if _, err := os.Stat(config.Path()); err == nil {
		cfgExists = true
	}
	printDeletePlan(inv, wipeDisk, wipeConfig, cfgExists)

	answer, err := prompt.String(fmt.Sprintf("Type the VM name (%s) to confirm", config.VMName), "")
	if err != nil {
		return err
	}
	if answer != config.VMName {
		return errors.New("not confirmed, nothing deleted")
	}

	if err := m.Delete(ctx, wipeDisk); err != nil {
		return err
	}
	if wipeConfig {
		existed, err := config.Remove()
		if err != nil {
			return err
		}
		if existed {
			fmt.Printf("%s removed.\n", config.Path())
		} else {
			fmt.Printf("%s does not exist, skipped.\n", config.Path())
		}
	}
	fmt.Println("Done.")
	return nil
}

func printDeletePlan(inv *vm.Inventory, wipeDisk, wipeConfig, cfgExists bool) {
	fmt.Println("This will delete:")
	if inv.State == vm.Missing {
		fmt.Printf("  VM %s: not defined, nothing to do\n", config.VMName)
	} else {
		fmt.Printf("  VM %s (%s): shut down (forced off after 30s) and removed from libvirt (virsh undefine)\n", config.VMName, inv.State)
	}
	if wipeDisk {
		if inv.DataDirExists {
			fmt.Printf("  data_dir %s (%s), recursively:\n", inv.DataDir, humanSize(inv.Size))
			w := 0
			for _, e := range inv.Entries {
				w = max(w, len(e.Path))
			}
			for _, e := range inv.Entries {
				fmt.Printf("    %-*s %10s\n", w, e.Path, humanSize(e.Size))
			}
		} else {
			fmt.Printf("  data_dir %s: does not exist, nothing to do\n", inv.DataDir)
		}
		if len(inv.Pools) == 0 {
			fmt.Println("  storage pool for data_dir: none defined, nothing to do")
		}
		for _, p := range inv.Pools {
			fmt.Printf("  libvirt storage pool %s (target %s)\n", p, inv.DataDir)
		}
	}
	if wipeConfig {
		if cfgExists {
			fmt.Printf("  config %s (incl. bot token and Telegram ids)\n", config.Path())
		} else {
			fmt.Printf("  config %s: does not exist, nothing to do\n", config.Path())
		}
	}
	fmt.Println()
	if wipeDisk {
		bar := strings.Repeat("!", 76)
		fmt.Println(bar)
		fmt.Println("!! ALL Projects, Sessions and uncommitted or unpushed work in the Workspace")
		fmt.Println("!! are lost IRREVOCABLY. Push everything you want to keep first.")
		fmt.Println(bar)
	} else {
		fmt.Println("The disk, data_dir and config are kept; `tg-agent start` re-creates the VM")
		fmt.Println("on the existing disk (Workspace, Projects and Sessions survive).")
	}
	fmt.Println()
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
