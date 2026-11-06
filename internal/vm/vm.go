// Package vm manages the single tg-agent libvirt domain by shelling out to
// virsh / virt-install / qemu-img.
package vm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dmoebius/tg-agent/internal/config"
	"github.com/dmoebius/tg-agent/internal/provision"
)

type State string

const (
	Missing State = "missing"
	Running State = "running"
	Stopped State = "shut off"
)

type Manager struct {
	Cfg *config.Config
	Out io.Writer
}

func (m *Manager) dir() string        { return m.Cfg.VM.DataDir }
func (m *Manager) baseImg() string    { return filepath.Join(m.dir(), "base.img") }
func (m *Manager) disk() string       { return filepath.Join(m.dir(), "disk.qcow2") }
func (m *Manager) KeyPath() string    { return filepath.Join(m.dir(), "id_ed25519") }
func (m *Manager) KnownHosts() string { return filepath.Join(m.dir(), "known_hosts") }

func (m *Manager) run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

func (m *Manager) virsh(ctx context.Context, args ...string) (string, error) {
	return m.run(ctx, "virsh", append([]string{"-c", m.Cfg.VM.LibvirtURI}, args...)...)
}

func (m *Manager) State(ctx context.Context) (State, error) {
	out, err := m.virsh(ctx, "domstate", config.VMName)
	if err != nil {
		if strings.Contains(out, "failed to get domain") || strings.Contains(out, "Domain not found") {
			return Missing, nil
		}
		return "", err
	}
	if strings.TrimSpace(out) == string(Running) {
		return Running, nil
	}
	return Stopped, nil
}

// Create downloads the image, prepares the disk and defines + starts the VM.
func (m *Manager) Create(ctx context.Context) error {
	for _, bin := range []string{"virsh", "virt-install", "qemu-img", "ssh", "ssh-keygen"} {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("required tool %q not found in PATH", bin)
		}
	}
	if err := os.MkdirAll(m.dir(), 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(m.KeyPath()); errors.Is(err, os.ErrNotExist) {
		if _, err := m.run(ctx, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "tg-agent", "-f", m.KeyPath()); err != nil {
			return err
		}
	}
	if _, err := os.Stat(m.baseImg()); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(m.Out, "Downloading %s ...\n", m.Cfg.VM.ImageURL)
		if err := download(ctx, m.Cfg.VM.ImageURL, m.baseImg()); err != nil {
			return err
		}
	}
	_ = os.Remove(m.disk())
	if _, err := m.run(ctx, "qemu-img", "create", "-f", "qcow2", "-F", "qcow2",
		"-b", m.baseImg(), m.disk(), fmt.Sprintf("%dG", m.Cfg.VM.DiskGB)); err != nil {
		return err
	}
	pub, err := os.ReadFile(m.KeyPath() + ".pub")
	if err != nil {
		return err
	}
	ud := filepath.Join(m.dir(), "user-data")
	md := filepath.Join(m.dir(), "meta-data")
	if err := os.WriteFile(ud, []byte(provision.UserData(m.Cfg, string(pub))), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(md, []byte("instance-id: "+config.VMName+"\n"), 0o600); err != nil {
		return err
	}
	fmt.Fprintln(m.Out, "Creating VM ...")
	_, err = m.run(ctx, "virt-install",
		"--connect", m.Cfg.VM.LibvirtURI,
		"--name", config.VMName,
		"--vcpus", fmt.Sprint(m.Cfg.VM.CPUs),
		"--memory", fmt.Sprint(m.Cfg.VM.MemoryMB),
		"--disk", "path="+m.disk()+",bus=virtio",
		"--import",
		"--os-variant", m.Cfg.VM.OSVariant,
		"--network", "network="+m.Cfg.VM.Network,
		"--graphics", "none",
		"--noautoconsole",
		"--cloud-init", "user-data="+ud+",meta-data="+md,
	)
	return err
}

func (m *Manager) Start(ctx context.Context) error {
	_, err := m.virsh(ctx, "start", config.VMName)
	return err
}

// Stop shuts the guest down gracefully, forcing off after timeout seconds.
func (m *Manager) Stop(ctx context.Context) error {
	if _, err := m.virsh(ctx, "shutdown", config.VMName); err != nil {
		return err
	}
	for i := 0; i < 60; i++ {
		st, err := m.State(ctx)
		if err != nil || st != Running {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-after(1):
		}
	}
	_, err := m.virsh(ctx, "destroy", config.VMName)
	return err
}

var ipRe = regexp.MustCompile(`(\d+\.\d+\.\d+\.\d+)/\d+`)

// IP returns the guest's IPv4 address from the DHCP lease.
func (m *Manager) IP(ctx context.Context) (string, error) {
	out, err := m.virsh(ctx, "domifaddr", config.VMName, "--source", "lease")
	if err != nil {
		return "", err
	}
	return parseIP(out)
}

func parseIP(out string) (string, error) {
	if mm := ipRe.FindStringSubmatch(out); mm != nil {
		return mm[1], nil
	}
	return "", errors.New("no IP address (yet)")
}

func download(ctx context.Context, url, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
