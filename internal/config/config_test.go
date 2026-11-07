package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestLoadAndTakopi(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte("[telegram]\nbot_token=\"abc\"\nchat_id=-100123\nallowed_user_ids=[1,2]\n"), 0o600)
	c, err := LoadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.VM.CPUs != 4 || c.VM.LibvirtURI != "qemu:///system" {
		t.Fatalf("defaults not applied: %+v", c.VM)
	}
	out := c.TakopiTOML()
	var parsed map[string]any
	if _, err := toml.Decode(out, &parsed); err != nil {
		t.Fatalf("invalid takopi toml: %v\n%s", err, out)
	}
	if !strings.Contains(out, "allowed_user_ids = [1, 2]") || !strings.Contains(out, "enabled = true") {
		t.Fatal(out)
	}
}

func TestValidate(t *testing.T) {
	valid := func() *Config {
		return &Config{Telegram: Telegram{BotToken: "t", ChatID: -1, AllowedUserIDs: []int64{7}}}
	}
	if err := valid().Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Config){
		"empty":             func(c *Config) { *c = Config{} },
		"no allowed users":  func(c *Config) { c.Telegram.AllowedUserIDs = nil },
		"bad alias":         func(c *Config) { c.Projects = []Project{{Alias: "../x", URL: "u"}} },
		"duplicate alias":   func(c *Config) { c.Projects = []Project{{Alias: "a", URL: "u"}, {Alias: "a", URL: "v"}} },
		"project url empty": func(c *Config) { c.Projects = []Project{{Alias: "a"}} },
	} {
		c := valid()
		mutate(c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestTakopiProjects(t *testing.T) {
	c := &Config{
		Telegram: Telegram{BotToken: "t", ChatID: -1, AllowedUserIDs: []int64{7}},
		Projects: []Project{
			{Alias: "tg-agent", URL: "git@github.com:dmoebius/tg-agent.git", WorktreeBase: "main"},
			{Alias: "other", URL: "https://example.com/other.git"},
		},
	}
	var parsed struct {
		Projects map[string]struct {
			Path         string `toml:"path"`
			WorktreesDir string `toml:"worktrees_dir"`
			WorktreeBase string `toml:"worktree_base"`
		} `toml:"projects"`
	}
	if _, err := toml.Decode(c.TakopiTOML(), &parsed); err != nil {
		t.Fatalf("invalid takopi toml: %v\n%s", err, c.TakopiTOML())
	}
	p := parsed.Projects["tg-agent"]
	if p.Path != "/home/agent/work/tg-agent" || p.WorktreesDir != ".worktrees" || p.WorktreeBase != "main" {
		t.Fatalf("%+v", parsed.Projects)
	}
	if o := parsed.Projects["other"]; o.Path != "/home/agent/work/other" || o.WorktreeBase != "" {
		t.Fatalf("%+v", parsed.Projects)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	c.Telegram = Telegram{BotToken: "t", ChatID: -1, AllowedUserIDs: []int64{7}}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	c2, err := Load()
	if err != nil || c2.Validate() != nil || c2.Telegram.AllowedUserIDs[0] != 7 {
		t.Fatalf("%+v %v", c2, err)
	}
	if fi, _ := os.Stat(Path()); fi.Mode().Perm() != 0o600 {
		t.Fatal("perm")
	}
}
