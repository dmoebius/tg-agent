package main

import (
	"context"
	"errors"
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
		var exit *vm.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.Code)
		}
		if errors.Is(err, prompt.ErrInterrupted) {
			os.Exit(130) // like a shell after Ctrl-C
		}
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
		&cobra.Command{
			Use:     "shell [<command> [<args>...]]",
			Aliases: []string{"sh"},
			Short:   "Open a login shell in the VM, or run a command there",
			Long: `Without arguments, opens an interactive login shell in the VM as user agent.
With arguments, runs <command> <args> in a login shell in the VM and exits with
its exit code. Everything after "shell" belongs to the remote command, flags too.`,
			DisableFlagParsing: true,
			RunE:               runShell,
		},
		newDeleteCmd(),
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
  2. Create a group and enable "Topics" in its settings.`)
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
	fmt.Printf(`  4. Add the bot to the group as administrator: open %s,
     pick the group and confirm ("Manage Topics" is the only right it needs).
  5. Now send a message mentioning @%s in any topic of the forum (waiting up to 3 minutes) ...
`, telegram.AdminLink(name), name)
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

// checkForum verifies the bot may manage Topics in the Forum (Takopi refuses
// to start otherwise) and returns the Forum's title. Interactively it waits
// for the user to fix the bot's rights instead of failing.
func checkForum(ctx context.Context, t config.Telegram) (string, error) {
	tg := telegram.Client{Token: t.BotToken}
	title, err := tg.CheckForum(ctx, t.ChatID)
	if err == nil || !prompt.Interactive() {
		return title, err
	}
	fmt.Println(err)
	fmt.Println("Waiting until this is fixed (up to 10 minutes, Ctrl-C to abort) ...")
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(5 * time.Second):
		}
		if title, err = tg.CheckForum(ctx, t.ChatID); err == nil {
			fmt.Println("Bot rights OK.")
			return title, nil
		}
	}
	return "", err
}

func runStart(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	m, err := setup(ctx)
	if err != nil {
		return err
	}
	fmt.Println("VM starting...")
	forum, err := checkForum(ctx, m.Cfg.Telegram)
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
	const topic = "General"
	fmt.Printf(`
VM is ready. Telegram forum %[1]q, topic %[2]q is connected to Claude inside the VM.
Ask Claude the first question in %[2]q.
To register a project ask Claude to checkout the repo, then execute `+"`! cd ~/work/<repo> && takopi init <alias>`"+`,
then open a topic for it with `+"`/topic <alias> @<branch>`"+`.
Use `+"`tg-agent status`"+` to get further connection infos.
`, forum, topic)
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
	if err := m.Stop(ctx); err != nil {
		return err
	}
	fmt.Println("VM stopped.")
	return nil
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
	field := func(label string, v any) { fmt.Printf("%-10s%v\n", label+":", v) }
	field("Config", config.Path())
	field("data_dir", m.Cfg.VM.DataDir)
	field("Forum", telegram.ForumWebLink(m.Cfg.Telegram.ChatID))
	field("VM", st)
	if st != vm.Running {
		return nil
	}
	c, err := m.Connect(ctx, 0)
	if err != nil {
		field("SSH", "unreachable: "+err.Error())
		return nil
	}
	field("IP", c.Host)
	field("Takopi", c.TakopiStatus(ctx))
	field("SSH", c.SSHCommand())
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
	if _, err := checkForum(ctx, m.Cfg.Telegram); err != nil {
		return err
	}
	c, err := m.Connect(ctx, 0)
	if err != nil {
		return err
	}
	return c.Update(ctx, m.Cfg.Telegram)
}

func runShell(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	m, err := setup(ctx)
	if err != nil {
		return err
	}
	if st, err := m.State(ctx); err != nil {
		return err
	} else if st != vm.Running {
		return fmt.Errorf("VM is %s; run `tg-agent start` first", st)
	}
	c, err := m.Connect(ctx, 0)
	if err != nil {
		return err
	}
	return c.Shell(ctx, prompt.Interactive(), args)
}
