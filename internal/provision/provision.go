// Package provision renders the cloud-init payload for the VM.
package provision

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/dmoebius/tg-agent/internal/config"
)

//go:embed provision.sh
var script string

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// UserData renders cloud-init user-data. The heavy lifting is done by provision.sh.
func UserData(c *config.Config, sshPubKey string) string {
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
  - path: /opt/tg-agent/takopi.toml
    permissions: "0600"
    encoding: b64
    content: %s
  - path: /opt/tg-agent/provision.sh
    permissions: "0700"
    encoding: b64
    content: %s
runcmd:
  - [bash, /opt/tg-agent/provision.sh]
`, config.VMName, strings.TrimSpace(sshPubKey), b64(c.TakopiTOML()), b64(script))
	return b.String()
}
