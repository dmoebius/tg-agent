package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/dmoebius/tg-agent/internal/config"
	"github.com/dmoebius/tg-agent/internal/prompt"
	"github.com/dmoebius/tg-agent/internal/vm"
)

// Delete shuts the VM down and removes it from libvirt. wipeDisk also removes
// data_dir and its storage pool, wipeConfig also the config directory. It
// shows what will be destroyed and asks for confirmation; it refuses to run
// without a terminal.
func Delete(ctx context.Context, wipeDisk, wipeConfig bool) error {
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
	if _, err := os.Stat(config.Dir()); err == nil {
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
			fmt.Printf("%s removed.\n", config.Dir())
		} else {
			fmt.Printf("%s does not exist, skipped.\n", config.Dir())
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
			fmt.Printf("  config directory %s, completely (incl. config.toml with bot token and Telegram ids)\n", config.Dir())
		} else {
			fmt.Printf("  config directory %s: does not exist, nothing to do\n", config.Dir())
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
