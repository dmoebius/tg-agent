#!/bin/bash
# Runs once as root via cloud-init. Installs dev tools, Claude Code, Takopi.
set -euxo pipefail
export DEBIAN_FRONTEND=noninteractive
A=agent
H=/home/$A
as_agent() { sudo -u $A -H bash -lc "$1"; }

apt-get update
apt-get install -y build-essential git curl wget unzip zip jq ca-certificates \
  python3 python3-venv python3-pip pipx

# Node.js LTS + pnpm
curl -fsSL https://deb.nodesource.com/setup_lts.x | bash -
apt-get install -y nodejs
npm install -g pnpm

# Go (latest stable)
GOV=$(curl -fsSL 'https://go.dev/VERSION?m=text' | head -1)
curl -fsSL "https://go.dev/dl/${GOV}.linux-amd64.tar.gz" | tar -C /usr/local -xz

# Playwright (browsers shared system-wide)
cat >/etc/profile.d/tg-agent.sh <<'EOT'
export PLAYWRIGHT_BROWSERS_PATH=/opt/pw-browsers
export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin:$HOME/.local/bin
EOT
export PLAYWRIGHT_BROWSERS_PATH=/opt/pw-browsers
npm install -g playwright
playwright install --with-deps chromium
chmod -R a+rX /opt/pw-browsers

# SDKMAN + Java, uv, Claude Code, Takopi (as agent)
as_agent 'curl -s https://get.sdkman.io | bash'
as_agent 'echo "source ~/.sdkman/bin/sdkman-init.sh" >> ~/.profile'
as_agent 'source ~/.sdkman/bin/sdkman-init.sh && sdk install java < /dev/null'
as_agent 'curl -LsSf https://astral.sh/uv/install.sh | sh'
as_agent 'curl -fsSL https://claude.ai/install.sh | bash'
as_agent 'uv tool install -U takopi'

# Takopi config + service
install -d -o $A -g $A -m 700 $H/.takopi
install -o $A -g $A -m 600 /opt/tg-agent/takopi.toml $H/.takopi/takopi.toml
install -d -o $A -g $A $H/work
cat >/etc/systemd/system/takopi.service <<EOT
[Unit]
Description=Takopi (Telegram <-> Claude)
After=network-online.target
Wants=network-online.target

[Service]
User=$A
WorkingDirectory=$H/work
EnvironmentFile=-/opt/tg-agent/env
ExecStart=/bin/bash -lc 'exec takopi'
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOT
systemctl daemon-reload
systemctl enable --now takopi
