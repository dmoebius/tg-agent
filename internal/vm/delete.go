package vm

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/dmoebius/tg-agent/internal/config"
)

// deleteShutdownTimeout is how long Delete waits for a graceful shutdown
// before forcing the VM off.
const deleteShutdownTimeout = 30 * time.Second

// Entry is a top-level entry of the data dir with its disk usage.
type Entry struct {
	Path string
	Size int64
}

// Inventory describes what Delete would remove.
type Inventory struct {
	State   State  // Missing if the domain is not defined
	DataDir string // as configured
	// DataDirExists is false if DataDir does not exist; Entries and Size are
	// empty then.
	DataDirExists bool
	Entries       []Entry
	Size          int64
	// Pools are the libvirt storage pools whose target path is DataDir
	// (virt-install defines one implicitly for the disk's directory).
	Pools []string
}

// Inventory collects what Delete would remove, without changing anything.
func (m *Manager) Inventory(ctx context.Context) (*Inventory, error) {
	st, err := m.State(ctx)
	if err != nil {
		return nil, err
	}
	inv := &Inventory{State: st, DataDir: m.dir()}
	if _, err := os.Stat(m.dir()); err == nil {
		inv.DataDirExists = true
		if inv.Entries, err = dirEntries(m.dir()); err != nil {
			return nil, err
		}
		for _, e := range inv.Entries {
			inv.Size += e.Size
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if inv.Pools, err = m.dataDirPools(ctx); err != nil {
		return nil, err
	}
	return inv, nil
}

// Delete removes the VM instance: it shuts the VM down (forcing it off after
// 30 seconds) and undefines it. With wipeDisk it also removes the storage
// pools pointing at the data dir and the data dir itself. Anything that does
// not exist is skipped.
func (m *Manager) Delete(ctx context.Context, wipeDisk bool) error {
	if wipeDisk {
		// Check before touching the VM, so a bad data_dir aborts early.
		if err := CheckDataDir(m.dir()); err != nil {
			return err
		}
	}
	if err := m.deleteDomain(ctx); err != nil {
		return err
	}
	if !wipeDisk {
		return nil
	}
	pools, err := m.dataDirPools(ctx)
	if err != nil {
		return err
	}
	for _, p := range pools {
		active, err := m.virsh(ctx, "pool-list", "--name")
		if err != nil {
			return err
		}
		if hasDomain(active, p) {
			if _, err := m.virsh(ctx, "pool-destroy", p); err != nil {
				return err
			}
		}
		if _, err := m.virsh(ctx, "pool-undefine", p); err != nil {
			return err
		}
		fmt.Fprintf(m.Out, "Storage pool %s removed.\n", p)
	}
	if _, err := os.Stat(m.dir()); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(m.Out, "%s does not exist, skipped.\n", m.dir())
		return nil
	}
	if err := os.RemoveAll(m.dir()); err != nil {
		return err
	}
	fmt.Fprintf(m.Out, "%s removed.\n", m.dir())
	return nil
}

func (m *Manager) deleteDomain(ctx context.Context) error {
	st, err := m.State(ctx)
	if err != nil {
		return err
	}
	if st == Missing {
		fmt.Fprintln(m.Out, "VM is not defined, skipped.")
		return nil
	}
	ds, err := m.domstate(ctx)
	if err != nil {
		return err
	}
	switch ds {
	case "shut off":
	case string(Running):
		fmt.Fprintln(m.Out, "VM shutting down...")
		forced, err := m.shutdown(ctx, deleteShutdownTimeout)
		if err != nil {
			return err
		}
		if forced {
			fmt.Fprintln(m.Out, "VM destroyed.")
		} else {
			fmt.Fprintln(m.Out, "VM stopped.")
		}
	default:
		// paused, crashed, ...: a graceful shutdown would not work, and
		// undefining an active domain only makes it transient.
		if _, err := m.virsh(ctx, "destroy", config.VMName); err != nil {
			return err
		}
		fmt.Fprintln(m.Out, "VM destroyed.")
	}
	// --nvram also removes a UEFI variable store, if any; without it,
	// undefine refuses such domains.
	if _, err := m.virsh(ctx, "undefine", "--nvram", config.VMName); err != nil {
		return err
	}
	fmt.Fprintln(m.Out, "VM removed from libvirt.")
	return nil
}

// dataDirPools returns the storage pools whose target path is the data dir.
// Pools of the same name pointing elsewhere are not the user's tg-agent pool
// and are left alone.
func (m *Manager) dataDirPools(ctx context.Context) ([]string, error) {
	names, err := m.virsh(ctx, "pool-list", "--all", "--name")
	if err != nil {
		return nil, err
	}
	want := canonical(m.dir())
	var out []string
	for _, n := range strings.Split(names, "\n") {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		x, err := m.virsh(ctx, "pool-dumpxml", n)
		if err != nil {
			return nil, err
		}
		if p, ok := poolTarget(x); ok && canonical(p) == want {
			out = append(out, n)
		}
	}
	return out, nil
}

// poolTarget extracts <target><path> from a pool's XML.
func poolTarget(poolXML string) (string, bool) {
	var p struct {
		Path string `xml:"target>path"`
	}
	if err := xml.Unmarshal([]byte(poolXML), &p); err != nil || p.Path == "" {
		return "", false
	}
	return p.Path, true
}

// canonical cleans p and resolves symlinks where possible.
func canonical(p string) string {
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// CheckDataDir refuses data_dir values whose recursive removal would destroy
// more than tg-agent's own files.
func CheckDataDir(dir string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return checkDataDir(dir, home, config.Dir())
}

func checkDataDir(dir, home, configDir string) error {
	refuse := func(why string) error {
		return fmt.Errorf("refusing to remove data_dir %q: %s", dir, why)
	}
	if !filepath.IsAbs(dir) {
		return refuse("not an absolute path")
	}
	d := canonical(dir)
	if d == "/" || filepath.Dir(d) == "/" {
		return refuse("it is the root or a top-level directory")
	}
	if within(canonical(home), d) {
		return refuse("it is the home directory or one of its parents")
	}
	if within(canonical(configDir), d) {
		return refuse("it contains the tg-agent config")
	}
	return nil
}

// within reports whether p is dir or lies below it.
func within(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

// dirEntries returns the top-level entries of dir with their disk usage
// (allocated blocks, so sparse disk images count with their real size).
func dirEntries(dir string) ([]Entry, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, de := range des {
		p := filepath.Join(dir, de.Name())
		size, err := diskUsage(p)
		if err != nil {
			return nil, err
		}
		out = append(out, Entry{Path: p, Size: size})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func diskUsage(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		total += allocated(fi)
		return nil
	})
	return total, err
}

func allocated(fi fs.FileInfo) int64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return st.Blocks * 512
	}
	return fi.Size()
}
