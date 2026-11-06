package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dmoebius/tg-agent/internal/config"
	"github.com/dmoebius/tg-agent/internal/ssh"
	"github.com/dmoebius/tg-agent/internal/vm"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := newRootCmd().ExecuteContext(ctx); err != nil {
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
		&cobra.Command{Use: "start", Short: "Create the VM on first run, start it and wait until Takopi runs", Args: cobra.NoArgs, RunE: runStart},
		&cobra.Command{Use: "stop", Short: "Shut the VM down", Args: cobra.NoArgs, RunE: runStop},
		&cobra.Command{Use: "status", Short: "Show VM and Takopi status", Args: cobra.NoArgs, RunE: runStatus},
		&cobra.Command{Use: "update", Short: "Update packages, Claude, Takopi and push the current config into the VM", Args: cobra.NoArgs, RunE: runUpdate},
	)
	return root
}

func setup() (*vm.Manager, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return &vm.Manager{Cfg: cfg, Out: os.Stdout}, nil
}

func sshClient(ctx context.Context, m *vm.Manager) (ssh.Client, error) {
	ip, err := m.IP(ctx)
	if err != nil {
		return ssh.Client{}, err
	}
	return ssh.Client{Host: ip, Key: m.KeyPath(), KnownHosts: m.KnownHosts()}, nil
}

func waitSSH(ctx context.Context, m *vm.Manager) (ssh.Client, error) {
	deadline := time.Now().Add(5 * time.Minute)
	for {
		if c, err := sshClient(ctx, m); err == nil {
			if _, err := c.Output(ctx, "true"); err == nil {
				return c, nil
			}
		}
		if time.Now().After(deadline) {
			return ssh.Client{}, errors.New("timed out waiting for SSH")
		}
		select {
		case <-ctx.Done():
			return ssh.Client{}, ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func runStart(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	m, err := setup()
	if err != nil {
		return err
	}
	st, err := m.State(ctx)
	if err != nil {
		return err
	}
	switch st {
	case vm.Missing:
		if err := m.Create(ctx); err != nil {
			return err
		}
	case vm.Stopped:
		if err := m.Start(ctx); err != nil {
			return err
		}
	case vm.Running:
		fmt.Println("VM already running")
	}
	c, err := waitSSH(ctx, m)
	if err != nil {
		return err
	}
	fmt.Println("Waiting for provisioning (cloud-init) ...")
	if err := c.Run(ctx, os.Stdout, nil, "cloud-init status --wait --long"); err != nil {
		return fmt.Errorf("provisioning failed, see /var/log/cloud-init-output.log in the VM: %w", err)
	}
	fmt.Println("Ready. Log in to Claude once if no API key is configured:")
	fmt.Println("  " + c.Command() + "   # then run: claude")
	return nil
}

func runStop(cmd *cobra.Command, _ []string) error {
	m, err := setup()
	if err != nil {
		return err
	}
	st, err := m.State(cmd.Context())
	if err != nil {
		return err
	}
	if st != vm.Running {
		fmt.Println("VM is", st)
		return nil
	}
	return m.Stop(cmd.Context())
}

func runStatus(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	m, err := setup()
	if err != nil {
		return err
	}
	st, err := m.State(ctx)
	if err != nil {
		return err
	}
	fmt.Println("VM:     ", st)
	if st != vm.Running {
		return nil
	}
	c, err := sshClient(ctx, m)
	if err != nil {
		fmt.Println("IP:      unknown")
		return nil
	}
	fmt.Println("IP:     ", c.Host)
	out, _ := c.Output(ctx, "systemctl is-active takopi")
	fmt.Println("Takopi: ", strings.TrimSpace(out))
	fmt.Println("SSH:    ", c.Command())
	return nil
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	m, err := setup()
	if err != nil {
		return err
	}
	if st, err := m.State(ctx); err != nil || st != vm.Running {
		return fmt.Errorf("VM must be running (state=%v, err=%v); run `tg-agent start`", st, err)
	}
	c, err := sshClient(ctx, m)
	if err != nil {
		return err
	}
	steps := []struct {
		desc, cmd string
		stdin     string
	}{
		{"config", "cat > ~/.takopi/takopi.toml && chmod 600 ~/.takopi/takopi.toml", m.Cfg.TakopiTOML()},
		{"env", "sudo tee /opt/tg-agent/env >/dev/null && sudo chmod 600 /opt/tg-agent/env", m.Cfg.ClaudeEnv()},
		{"apt", "sudo DEBIAN_FRONTEND=noninteractive apt-get update && sudo DEBIAN_FRONTEND=noninteractive apt-get -y upgrade", ""},
		{"claude", "bash -lc 'claude update || curl -fsSL https://claude.ai/install.sh | bash'", ""},
		{"takopi", "bash -lc 'uv tool upgrade takopi'", ""},
		{"restart", "sudo systemctl restart takopi", ""},
	}
	for _, s := range steps {
		fmt.Printf("==> %s\n", s.desc)
		var in io.Reader
		if s.stdin != "" {
			in = strings.NewReader(s.stdin)
		}
		if err := c.Run(ctx, os.Stdout, in, s.cmd); err != nil {
			return fmt.Errorf("%s: %w", s.desc, err)
		}
	}
	return nil
}
