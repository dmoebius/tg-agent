// Package config loads ~/.config/tg-agent/config.toml.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

const VMName = "tg-agent"

// WorkspaceDir is the persistent directory in the VM that holds all Projects.
const WorkspaceDir = "/home/agent/work"

type Config struct {
	Telegram Telegram  `toml:"telegram"`
	VM       VM        `toml:"vm"`
	Projects []Project `toml:"projects"`
}

// Project is a git repository that tg-agent clones into the Workspace and
// registers with Takopi under Alias, so Topics can be bound to it.
type Project struct {
	Alias        string `toml:"alias"`
	URL          string `toml:"url"`
	WorktreeBase string `toml:"worktree_base,omitempty"` // base branch for new worktrees
}

// Dir is the Project's checkout in the VM.
func (p Project) Dir() string { return WorkspaceDir + "/" + p.Alias }

type Telegram struct {
	BotToken       string  `toml:"bot_token"`
	ChatID         int64   `toml:"chat_id"` // forum-enabled supergroup
	AllowedUserIDs []int64 `toml:"allowed_user_ids"`
}

type VM struct {
	CPUs       int    `toml:"cpus"`
	MemoryMB   int    `toml:"memory_mb"`
	DiskGB     int    `toml:"disk_gb"`
	ImageURL   string `toml:"image_url"`
	LibvirtURI string `toml:"libvirt_uri"`
	Network    string `toml:"network"`
	OSVariant  string `toml:"os_variant"`
	DataDir    string `toml:"data_dir"`
}

func Dir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "tg-agent")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config", "tg-agent")
}

func Path() string { return filepath.Join(Dir(), "config.toml") }

// Load reads the config (empty + defaults if the file does not exist yet).
// It does not validate; call Validate and fill gaps interactively.
func Load() (*Config, error) {
	p := Path()
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		c := &Config{}
		c.applyDefaults()
		return c, nil
	}
	return LoadFile(p)
}

func LoadFile(p string) (*Config, error) {
	c := &Config{}
	if _, err := toml.DecodeFile(p, c); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	c.applyDefaults()
	return c, nil
}

// Save writes the config with mode 0600 (it contains the bot token).
func (c *Config) Save() error {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(c)
}

func (c *Config) applyDefaults() {
	v := &c.VM
	def := func(s *string, d string) {
		if *s == "" {
			*s = d
		}
	}
	defi := func(i *int, d int) {
		if *i == 0 {
			*i = d
		}
	}
	defi(&v.CPUs, 4)
	defi(&v.MemoryMB, 8192)
	defi(&v.DiskGB, 60)
	def(&v.ImageURL, "https://cloud-images.ubuntu.com/resolute/current/resolute-server-cloudimg-amd64.img")
	def(&v.LibvirtURI, "qemu:///system")
	def(&v.Network, "default")
	def(&v.OSVariant, "ubuntu24.04")
	h, _ := os.UserHomeDir()
	def(&v.DataDir, filepath.Join(h, ".local", "share", "tg-agent"))
	if strings.HasPrefix(v.DataDir, "~/") {
		v.DataDir = filepath.Join(h, v.DataDir[2:])
	}
}

var aliasRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

func (c *Config) Validate() error {
	if c.Telegram.BotToken == "" {
		return errors.New("telegram.bot_token is not set")
	}
	if c.Telegram.ChatID == 0 {
		return errors.New("telegram.chat_id is not set")
	}
	if len(c.Telegram.AllowedUserIDs) == 0 {
		return errors.New("telegram.allowed_user_ids is empty (anyone in the forum could control Claude)")
	}
	seen := map[string]bool{}
	for i, p := range c.Projects {
		if !aliasRe.MatchString(p.Alias) {
			return fmt.Errorf("projects[%d].alias %q is invalid (letters, digits, '-' and '_' only)", i, p.Alias)
		}
		if seen[p.Alias] {
			return fmt.Errorf("projects[%d].alias %q is not unique", i, p.Alias)
		}
		seen[p.Alias] = true
		if p.URL == "" {
			return fmt.Errorf("projects[%d].url is not set", i)
		}
	}
	return nil
}

// TakopiTOML renders ~/.takopi/takopi.toml for the VM (one Session per Topic,
// each Topic bound to a Project and branch worktree).
func (c *Config) TakopiTOML() string {
	ids := make([]string, len(c.Telegram.AllowedUserIDs))
	for i, id := range c.Telegram.AllowedUserIDs {
		ids[i] = fmt.Sprint(id)
	}
	var b strings.Builder
	fmt.Fprintf(&b, `transport = "telegram"
default_engine = "claude"
watch_config = true

[transports.telegram]
bot_token = %q
chat_id = %d
allowed_user_ids = [%s]
session_mode = "chat"
show_resume_line = false

[transports.telegram.topics]
enabled = true
scope = "main"
`, c.Telegram.BotToken, c.Telegram.ChatID, strings.Join(ids, ", "))
	for _, p := range c.Projects {
		fmt.Fprintf(&b, "\n[projects.%q]\npath = %q\nworktrees_dir = \".worktrees\"\n", p.Alias, p.Dir())
		if p.WorktreeBase != "" {
			fmt.Fprintf(&b, "worktree_base = %q\n", p.WorktreeBase)
		}
	}
	return b.String()
}
