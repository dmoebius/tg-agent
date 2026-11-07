package takopi

import (
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/dmoebius/tg-agent/internal/config"
)

var tg = config.Telegram{BotToken: "new-token", ChatID: -100123, AllowedUserIDs: []int64{1, 2}}

type parsed struct {
	Transport     string `toml:"transport"`
	DefaultEngine string `toml:"default_engine"`
	WatchConfig   bool   `toml:"watch_config"`
	Transports    struct {
		Telegram struct {
			BotToken       string  `toml:"bot_token"`
			ChatID         int64   `toml:"chat_id"`
			AllowedUserIDs []int64 `toml:"allowed_user_ids"`
			SessionMode    string  `toml:"session_mode"`
			ShowResumeLine bool    `toml:"show_resume_line"`
			Topics         struct {
				Enabled bool   `toml:"enabled"`
				Scope   string `toml:"scope"`
			} `toml:"topics"`
		} `toml:"telegram"`
	} `toml:"transports"`
	Projects map[string]struct {
		Path         string `toml:"path"`
		WorktreesDir string `toml:"worktrees_dir"`
		WorktreeBase string `toml:"worktree_base"`
	} `toml:"projects"`
	DefaultProject string `toml:"default_project"`
}

func merge(t *testing.T, existing string) parsed {
	t.Helper()
	out, err := Merge(existing, tg)
	if err != nil {
		t.Fatal(err)
	}
	var p parsed
	if _, err := toml.Decode(out, &p); err != nil {
		t.Fatalf("invalid toml: %v\n%s", err, out)
	}
	return p
}

func TestMergeFresh(t *testing.T) {
	p := merge(t, "")
	tt := p.Transports.Telegram
	if p.Transport != "telegram" || p.DefaultEngine != "claude" || !p.WatchConfig ||
		tt.BotToken != "new-token" || tt.ChatID != -100123 || len(tt.AllowedUserIDs) != 2 ||
		tt.SessionMode != "chat" || tt.ShowResumeLine || !tt.Topics.Enabled || tt.Topics.Scope != "main" {
		t.Fatalf("%+v", p)
	}
}

func TestMergePreservesTakopiOwnedKeys(t *testing.T) {
	p := merge(t, `
default_engine = "codex"
default_project = "foo"

[transports.telegram]
bot_token = "old-token"
chat_id = -1
allowed_user_ids = [9]
show_resume_line = true

[transports.telegram.topics]
enabled = false

[projects.foo]
path = "/home/agent/work/foo"
worktrees_dir = ".worktrees"
worktree_base = "main"
`)
	tt := p.Transports.Telegram
	if tt.BotToken != "new-token" || tt.ChatID != -100123 || len(tt.AllowedUserIDs) != 2 || !tt.Topics.Enabled {
		t.Fatalf("managed keys not enforced: %+v", tt)
	}
	if p.DefaultEngine != "codex" || !tt.ShowResumeLine || p.DefaultProject != "foo" {
		t.Fatalf("user settings overwritten: %+v", p)
	}
	foo := p.Projects["foo"]
	if foo.Path != "/home/agent/work/foo" || foo.WorktreesDir != ".worktrees" || foo.WorktreeBase != "main" {
		t.Fatalf("project lost: %+v", p.Projects)
	}
}

func TestMergeRejectsBrokenFile(t *testing.T) {
	for _, in := range []string{"not toml [", `transports = "x"`} {
		if _, err := Merge(in, tg); err == nil {
			t.Errorf("%q: expected error", in)
		}
	}
}
