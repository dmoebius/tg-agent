// Package provision defines the Provisioning payload for the VM and renders
// it as cloud-init user-data.
package provision

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/dmoebius/tg-agent/internal/config"
	"github.com/dmoebius/tg-agent/internal/takopi"
)

//go:embed provision.sh
var script string

// Dir is where the payload lives in the VM.
const Dir = "/opt/tg-agent"

// Command runs Provisioning in the VM (as root) once the payload is in place.
var Command = "bash " + path.Join(Dir, "provision.sh")

// File is one file of the payload, to be written into the VM.
type File struct {
	Path    string
	Mode    os.FileMode
	Content string
}

// Files returns the payload: takopi.toml (tg merged into currentTakopi, the
// VM's current ~/.takopi/takopi.toml, empty if none) and provision.sh.
func Files(tg config.Telegram, currentTakopi string) ([]File, error) {
	tk, err := takopi.Merge(currentTakopi, tg)
	if err != nil {
		return nil, err
	}
	return []File{
		{Path: path.Join(Dir, "takopi.toml"), Mode: 0o600, Content: tk},
		{Path: path.Join(Dir, "provision.sh"), Mode: 0o700, Content: script},
	}, nil
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// UserData renders cloud-init user-data for a new VM. The heavy lifting is
// done by provision.sh.
func UserData(c *config.Config, sshPubKey string) (string, error) {
	files, err := Files(c.Telegram, "")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, `#cloud-config
hostname: %s
users:
  - name: agent
    shell: /bin/bash
    sudo: ALL=(ALL) NOPASSWD:ALL
    ssh_authorized_keys:
      - %s
write_files:
`, config.VMName, strings.TrimSpace(sshPubKey))
	for _, f := range files {
		fmt.Fprintf(&b, `  - path: %s
    permissions: "%#o"
    encoding: b64
    content: %s
`, f.Path, f.Mode, b64(f.Content))
	}
	fmt.Fprintf(&b, "runcmd:\n  - %s\n", Command)
	return b.String(), nil
}
