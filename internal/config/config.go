// Package config loads ~/.config/tg-agent/config.toml.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const VMName = "tg-agent"

type Config struct {
	Telegram Telegram `toml:"telegram"`
	VM       VM       `toml:"vm"`
	Claude   Claude   `toml:"claude"`
}

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

type Claude struct {
	// Optional. If empty, run `claude` inside the VM once to log in.
	APIKey string `toml:"api_key"`
}

const Template = `# tg-agent configuration
[telegram]
bot_token = ""            # from @BotFather
chat_id = 0               # forum-enabled supergroup; bot must be admin with "Manage Topics"
allowed_user_ids = []     # Telegram user ids allowed to talk to the bot (strongly recommended)

[vm]
cpus = 4
memory_mb = 8192
disk_gb = 60
# image_url = "https://cloud-images.ubuntu.com/resolute/current/resolute-server-cloudimg-amd64.img"
# libvirt_uri = "qemu:///system"
# network = "default"
# os_variant = "ubuntu24.04"
# data_dir = "~/.local/share/tg-agent"

[claude]
api_key = ""              # optional
`

func Dir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "tg-agent")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config", "tg-agent")
}

func Path() string { return filepath.Join(Dir(), "config.toml") }

var ErrCreated = errors.New("config template written; please edit it and re-run")

// Load reads the config. If missing, a template is written and ErrCreated returned.
func Load() (*Config, error) {
	p := Path()
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, []byte(Template), 0o600); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s", ErrCreated, p)
	}
	return LoadFile(p)
}

func LoadFile(p string) (*Config, error) {
	c := &Config{}
	if _, err := toml.DecodeFile(p, c); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	c.applyDefaults()
	return c, c.Validate()
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

func (c *Config) Validate() error {
	if c.Telegram.BotToken == "" {
		return errors.New("telegram.bot_token is not set")
	}
	if c.Telegram.ChatID == 0 {
		return errors.New("telegram.chat_id is not set")
	}
	return nil
}

// TakopiTOML renders ~/.takopi/takopi.toml for the VM (topic-per-session).
func (c *Config) TakopiTOML() string {
	ids := make([]string, len(c.Telegram.AllowedUserIDs))
	for i, id := range c.Telegram.AllowedUserIDs {
		ids[i] = fmt.Sprint(id)
	}
	return fmt.Sprintf(`transport = "telegram"
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
}

// ClaudeEnv renders the systemd EnvironmentFile for the takopi service.
func (c *Config) ClaudeEnv() string {
	if c.Claude.APIKey == "" {
		return "\n"
	}
	return "ANTHROPIC_API_KEY=" + c.Claude.APIKey + "\n"
}
