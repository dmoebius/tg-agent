package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"

	"github.com/dmoebius/tg-agent/internal/config"
	"github.com/dmoebius/tg-agent/internal/preflight"
	"github.com/dmoebius/tg-agent/internal/prompt"
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
  1. Open https://web.telegram.org/ and chat with @BotFather, write message "/newbot"
     and follow instructions. Remember the bot token.
  2. Create a group, enable "Topics" in its settings, add the bot as admin with "Manage Topics".`)
	var err error
	if t.BotToken == "" {
		fmt.Println("  3. After that, enter the bot token here:")
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
	fmt.Printf("  4. Now send a message mentioning @%s in any topic of the forum (waiting up to 3 minutes) ...\n", name)
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

func runStart(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	m, err := setup(ctx)
	if err != nil {
		return err
	}
	if err := m.EnsureRunning(ctx); err != nil {
		return err
	}
	c, err := m.Connect(ctx, 5*time.Minute)
	if err != nil {
		return err
	}
	fmt.Println("Waiting for provisioning (cloud-init) ...")
	if err := c.WaitProvisioned(ctx); err != nil {
		return err
	}
	if err := c.EnsureClaudeAuth(ctx, prompt.Interactive()); err != nil {
		return err
	}
	fmt.Println("Ready. Register a project in the VM (`cd ~/work/<repo> && takopi init <alias>`, or ask Claude in the forum to do it),")
	fmt.Println("then open a topic for it with `/topic <alias> @<branch>`.")
	return nil
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
	c, err := m.Connect(ctx, 0)
	if err != nil {
		fmt.Println("SSH:     unreachable:", err)
		return nil
	}
	fmt.Println("IP:     ", c.Host)
	fmt.Println("Takopi: ", c.TakopiStatus(ctx))
	fmt.Println("SSH:    ", c.SSHCommand())
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
	c, err := m.Connect(ctx, 0)
	if err != nil {
		return err
	}
	return c.Update(ctx, m.Cfg.Telegram)
}
