package provision

import (
	"strings"
	"testing"

	"github.com/dmoebius/tg-agent/internal/config"
	"gopkg.in/yaml.v3"
)

func TestUserDataIsValidCloudConfig(t *testing.T) {
	ud := UserData(&config.Config{}, "ssh-ed25519 AAAA test")
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
