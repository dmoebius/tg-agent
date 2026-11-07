package vm

import (
	"context"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
)

// qemuUsers are the accounts QEMU runs as under qemu:///system
// (Debian/Ubuntu/Arch, Fedora).
var qemuUsers = []string{"libvirt-qemu", "qemu"}

// grantHypervisorAccess lets the system QEMU process reach the disk in the
// data dir, which lives in the user's home: like virt-manager, it adds a
// search-only ACL entry (u:<qemu>:x) to every directory on the path that the
// user owns and that others cannot traverse. Search permission does not allow
// listing the directory; the SSH key stays 0600.
func (m *Manager) grantHypervisorAccess(ctx context.Context) error {
	if !strings.HasSuffix(m.Cfg.VM.LibvirtURI, "/system") {
		return nil // qemu:///session runs as the user
	}
	name := ""
	for _, n := range qemuUsers {
		if _, err := user.Lookup(n); err == nil {
			name = n
			break
		}
	}
	if name == "" {
		return nil // unknown setup; virt-install will report what is missing
	}
	for _, d := range dirsToGrant(m.dir(), os.Getuid()) {
		if _, err := m.run(ctx, "setfacl", "-m", "u:"+name+":x", d); err != nil {
			return err
		}
	}
	return nil
}

// dirsToGrant returns dir and its parents that are owned by uid and lack
// search permission for others.
func dirsToGrant(dir string, uid int) []string {
	var out []string
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		if fi, err := os.Stat(d); err == nil {
			st, ok := fi.Sys().(*syscall.Stat_t)
			if ok && int(st.Uid) == uid && fi.Mode().Perm()&0o001 == 0 {
				out = append(out, d)
			}
		}
		if d == filepath.Dir(d) {
			return out
		}
	}
}
