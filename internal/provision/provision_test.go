package provision

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dmoebius/tg-agent/internal/config"
	"gopkg.in/yaml.v3"
)

func TestUserDataIsValidCloudConfig(t *testing.T) {
	ud, err := UserData(&config.Config{}, "ssh-ed25519 AAAA test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ud, "#cloud-config\n") {
		t.Fatal("missing header")
	}
	var v map[string]any
	if err := yaml.Unmarshal([]byte(ud), &v); err != nil {
		t.Fatal(err)
	}
	if len(v["write_files"].([]any)) != 2 {
		t.Fatal("write_files")
	}
}

func TestUserDataMatchesFiles(t *testing.T) {
	ud, err := UserData(&config.Config{}, "ssh-ed25519 AAAA test")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		WriteFiles []struct{ Path, Permissions string } `yaml:"write_files"`
		Runcmd     []string                             `yaml:"runcmd"`
	}
	if err := yaml.Unmarshal([]byte(ud), &v); err != nil {
		t.Fatal(err)
	}
	files, _ := Files(config.Telegram{}, "")
	for i, f := range files {
		got := v.WriteFiles[i]
		if got.Path != f.Path || got.Permissions != fmt.Sprintf("%#o", f.Mode) {
			t.Errorf("write_files[%d] = %+v, want %s %#o", i, got, f.Path, f.Mode)
		}
	}
	if len(v.Runcmd) != 1 || v.Runcmd[0] != Command {
		t.Errorf("runcmd = %v", v.Runcmd)
	}
}

func TestFilesKeepsTakopiProjects(t *testing.T) {
	files, err := Files(config.Telegram{BotToken: "tok"}, "[projects.foo]\npath = \"/x\"\n")
	if err != nil {
		t.Fatal(err)
	}
	tk := files[0].Content
	if !strings.Contains(tk, "projects.foo") || !strings.Contains(tk, `bot_token = "tok"`) {
		t.Fatalf("takopi.toml:\n%s", tk)
	}
}
