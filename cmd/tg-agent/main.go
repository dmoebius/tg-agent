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
	"github.com/dmoebius/tg-agent/internal/preflight"
	"github.com/dmoebius/tg-agent/internal/prompt"
	"github.com/dmoebius/tg-agent/internal/provision"
	"github.com/dmoebius/tg-agent/internal/ssh"
	"github.com/dmoebius/tg-agent/internal/takopi"
	"github.com/dmoebius/tg-agent/internal/telegram"
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
		&cobra.Command{Use: "update", Short: "Push Telegram settings, re-run provisioning (install and upgrade tools), restart Takopi", Args: cobra.NoArgs, RunE: runUpdate},
	)
	return root
}

// setup loads the config, asks interactively for anything missing and
// verifies the host tooling.
func setup(ctx context.Context) (*vm.Manager, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		t := cfg.Telegram
		if t.BotToken != "" && t.ChatID != 0 && len(t.AllowedUserIDs) > 0 {
			return nil, fmt.Errorf("%w (edit %s)", err, config.Path())
		}
		if !prompt.Interactive() {
			return nil, fmt.Errorf("%w (edit %s or run interactively)", err, config.Path())
		}
		if err := askTelegram(ctx, cfg); err != nil {
			return nil, err
		}
		if err := cfg.Save(); err != nil {
			return nil, err
		}
		fmt.Println("Saved", config.Path())
	}
	if err := preflight.Check(ctx, cfg.VM.LibvirtURI, cfg.VM.Network); err != nil {
		return nil, err
	}
	return &vm.Manager{Cfg: cfg, Out: os.Stdout}, nil
}

func askTelegram(ctx context.Context, cfg *config.Config) error {
	t := &cfg.Telegram
	fmt.Println(`Telegram setup:
  1. Create a bot with @BotFather and copy its token.
  2. Create a group, enable "Topics" in its settings, add the bot as admin with "Manage Topics".`)
	var err error
	if t.BotToken == "" {
		if t.BotToken, err = prompt.Secret("Bot token"); err != nil {
			return err
		}
	}
	var found telegram.Origin
	if t.ChatID == 0 || len(t.AllowedUserIDs) == 0 {
		found = discoverOrigin(ctx, t.BotToken)
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if t.ChatID == 0 {
		fmt.Println("  Chat id of the forum: forward a group message to @RawDataBot (looks like -100123456789).")
		if t.ChatID, err = prompt.Int64("Forum chat id", found.ChatID); err != nil {
			return err
		}
	}
	if len(t.AllowedUserIDs) == 0 {
		def := ""
		if found.UserID != 0 {
			def = fmt.Sprint(found.UserID)
		}
		for {
			s, err := prompt.String("Allowed Telegram user ids (comma separated; get yours from @userinfobot)", def)
			if err != nil {
				return err
			}
			if t.AllowedUserIDs, err = prompt.Int64List(s); err == nil && len(t.AllowedUserIDs) > 0 {
				break
			}
			fmt.Println("  at least one valid user id is required (anyone in the forum could otherwise control Claude)")
		}
	}
	return cfg.Validate()
}

// discoverOrigin waits for the user to post in the Forum and returns its chat
// id and the user's id as defaults for the prompts. Failures are not fatal:
// the user can still enter both ids manually.
func discoverOrigin(ctx context.Context, token string) telegram.Origin {
	tg := telegram.Client{Token: token}
	name, err := tg.BotUsername(ctx)
	if err != nil {
		fmt.Println("  could not verify the bot token:", err)
		return telegram.Origin{}
	}
	fmt.Printf("  3. Now send a message mentioning @%s in any topic of the forum (waiting up to 3 minutes) ...\n", name)
	dctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	o, err := tg.Discover(dctx)
	if err != nil {
		if ctx.Err() == nil {
			fmt.Println("  no message received, please enter the ids manually:", err)
		}
		return telegram.Origin{}
	}
	fmt.Printf("  Found forum %q (%d), sent by %s (%d)\n", o.ChatTitle, o.ChatID, o.UserName, o.UserID)
	return o
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
	m, err := setup(ctx)
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
	if err := ensureClaudeAuth(ctx, c); err != nil {
		return err
	}
	fmt.Println("Ready. Register a project in the VM (`cd ~/work/<repo> && takopi init <alias>`, or ask Claude in the forum to do it),")
	fmt.Println("then open a topic for it with `/topic <alias> @<branch>`.")
	return nil
}

// ensureClaudeAuth runs `claude auth login` (subscription) in the VM if not logged in yet.
func ensureClaudeAuth(ctx context.Context, c ssh.Client) error {
	if _, err := c.Output(ctx, `bash -lc 'claude auth status --text'`); err == nil {
		return nil
	}
	if !prompt.Interactive() {
		return fmt.Errorf("Claude is not logged in; run `%s` and then `claude auth login`", c.Command())
	}
	fmt.Println("Claude is not logged in. Starting `claude auth login` (Claude subscription) ...")
	if err := c.Interactive(ctx, `bash -lc 'claude auth login --claudeai'`); err != nil {
		return fmt.Errorf("claude auth login: %w", err)
	}
	_, err := c.Output(ctx, `bash -lc 'claude auth status --text'`)
	if err != nil {
		return errors.New("Claude login did not complete")
	}
	_, err = c.Output(ctx, "sudo systemctl restart takopi")
	return err
}

func runStop(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	m, err := setup(ctx)
	if err != nil {
		return err
	}
	st, err := m.State(ctx)
	if err != nil {
		return err
	}
	if st != vm.Running {
		fmt.Println("VM is", st)
		return nil
	}
	return m.Stop(ctx)
}

func runStatus(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	m, err := setup(ctx)
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
	m, err := setup(ctx)
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
	// Takopi owns takopi.toml (Projects etc.); only merge our keys into it.
	current, err := c.Output(ctx, "cat ~/.takopi/takopi.toml 2>/dev/null || true")
	if err != nil {
		return fmt.Errorf("read takopi.toml: %w", err)
	}
	tk, err := takopi.Merge(current, m.Cfg.Telegram)
	if err != nil {
		return err
	}
	// Provisioning installs the uploaded takopi.toml and upgrades or installs
	// every tool, so a changed provision.sh takes effect here too.
	const upload = "sudo install -d -m 700 /opt/tg-agent && sudo sh -c 'umask 077 && cat > /opt/tg-agent/%s'"
	steps := []struct {
		desc, cmd string
		stdin     string
	}{
		{"config", fmt.Sprintf(upload, "takopi.toml"), tk},
		{"provision script", fmt.Sprintf(upload, "provision.sh"), provision.Script()},
		{"apt", "sudo DEBIAN_FRONTEND=noninteractive apt-get update && sudo DEBIAN_FRONTEND=noninteractive apt-get -y upgrade", ""},
		{"provision", "sudo bash /opt/tg-agent/provision.sh", ""},
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
	fmt.Println("==> restart")
	if err := c.Run(ctx, os.Stdout, nil, "sudo systemctl restart takopi"); err != nil {
		return fmt.Errorf("restart: %w", err)
	}
	return nil
}
