#!/bin/bash
# Provisioning: runs as root, first via cloud-init and again on every
# `tg-agent update`. Must stay idempotent: upgrades the system packages,
# installs missing tools and upgrades existing ones. Expects /opt/tg-agent/takopi.toml.
set -euxo pipefail
# Never ask: a prompt would hang `tg-agent update` / cloud-init without any
# visible reason. debconf takes its defaults, ucf and dpkg keep locally
# changed config files, needrestart restarts services automatically.
export DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a UCF_FORCE_CONFFOLD=1
cat >/etc/apt/apt.conf.d/90tg-agent <<'EOT'
APT::Get::Assume-Yes "true";
Dpkg::Options { "--force-confdef"; "--force-confold"; };
EOT
A=agent
H=/home/$A
as_agent() { sudo -u $A -H bash -lc "$1"; }

# Upgrade the system first (security updates).
apt-get update
apt-get upgrade
apt-get install -y build-essential git curl wget unzip zip jq ca-certificates \
  python3 python3-venv python3-pip pipx

# GitHub CLI (official apt repo; upgraded by apt)
install -d -m 755 /etc/apt/keyrings
curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg -o /etc/apt/keyrings/githubcli-archive-keyring.gpg
chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
  >/etc/apt/sources.list.d/github-cli.list
apt-get update
apt-get install -y gh

# GitLab CLI (latest .deb from the GitLab release; installed only when newer)
GLAB=$(curl -fsSL 'https://gitlab.com/api/v4/projects/gitlab-org%2Fcli/releases/permalink/latest')
GLABV=$(jq -r '.tag_name | ltrimstr("v")' <<<"$GLAB")
if [ "$(dpkg-query -W -f='${Version}' glab 2>/dev/null || true)" != "$GLABV" ]; then
  GLABDEB=$(mktemp --suffix=.deb) && chmod 644 "$GLABDEB"
  curl -fsSL -o "$GLABDEB" "$(jq -r '.assets.links[] | select(.name | endswith("_linux_amd64.deb")) | .url' <<<"$GLAB")"
  apt-get install -y "$GLABDEB"
  rm -f "$GLABDEB"
fi

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
# The browser binaries sit in versioned subdirectories (chromium-<rev>/chrome-linux*/),
# so put the newest Chromium on PATH via a stable symlink instead.
ln -sfn "$(ls -d /opt/pw-browsers/chromium-[0-9]*/chrome-linux*/chrome | sort -V | tail -1)" /usr/local/bin/chromium

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
