#!/bin/bash
# Provisioning: runs as root, first via cloud-init and again on every
# `tg-agent update`. Must stay idempotent: installs missing tools and
# upgrades existing ones. Expects /opt/tg-agent/takopi.toml.
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

# Go (latest stable; replaced only when a newer release exists)
GOV=$(curl -fsSL 'https://go.dev/VERSION?m=text' | head -1)
if [ "$(head -1 /usr/local/go/VERSION 2>/dev/null || true)" != "$GOV" ]; then
  rm -rf /usr/local/go
  curl -fsSL "https://go.dev/dl/${GOV}.linux-amd64.tar.gz" | tar -C /usr/local -xz
fi

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
as_agent '[ -d ~/.sdkman ] || curl -s https://get.sdkman.io | bash'
as_agent 'grep -qxF "source ~/.sdkman/bin/sdkman-init.sh" ~/.profile || echo "source ~/.sdkman/bin/sdkman-init.sh" >> ~/.profile'
as_agent 'source ~/.sdkman/bin/sdkman-init.sh && { [ -e ~/.sdkman/candidates/java/current ] || sdk install java < /dev/null; }'
as_agent '{ command -v uv >/dev/null && uv self update; } || curl -LsSf https://astral.sh/uv/install.sh | sh'
as_agent '{ command -v claude >/dev/null && claude update; } || curl -fsSL https://claude.ai/install.sh | bash'
as_agent 'uv tool install -U takopi'

# Git: keep Takopi's per-branch worktrees out of `git status`
as_agent 'mkdir -p ~/.config/git && touch ~/.config/git/ignore && git config --global core.excludesfile ~/.config/git/ignore'
as_agent 'grep -qxF ".worktrees/" ~/.config/git/ignore || echo ".worktrees/" >> ~/.config/git/ignore'

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
ExecStart=/bin/bash -lc 'exec takopi'
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOT
systemctl daemon-reload
systemctl enable --now takopi
