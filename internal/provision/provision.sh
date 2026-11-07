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

# Upgrade the system first (security updates). full-upgrade, unlike upgrade,
# also installs the new packages a kernel update needs instead of keeping it
# back; autoremove then drops all but the running and the newest kernel.
apt-get update
apt-get full-upgrade
apt-get autoremove --purge
apt-get install -y build-essential git curl wget unzip zip jq ca-certificates gnupg \
  python3 python3-venv python3-pip pipx

# Install software from signed apt repositories (Ubuntu or the vendor's)
# wherever possible, never by piping a downloaded install script into a shell.
install -d -m 755 /etc/apt/keyrings

# repo NAME KEY_URL SOURCE_LINE: register a vendor apt repository signed with
# the key at KEY_URL (ASCII-armored or binary).
repo() {
  local key=/etc/apt/keyrings/$1.gpg
  curl -fsSL -o "$key.tmp" "$2"
  if grep -q -- '-----BEGIN PGP PUBLIC KEY BLOCK-----' "$key.tmp"; then
    gpg --dearmor --yes -o "$key" "$key.tmp" && rm -f "$key.tmp"
  else
    mv -f "$key.tmp" "$key"
  fi
  chmod 644 "$key"
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/$1.gpg] $3" >"/etc/apt/sources.list.d/$1.list"
}

# GitHub CLI (GitHub's repo; Ubuntu's gh is far behind)
repo github-cli https://cli.github.com/packages/githubcli-archive-keyring.gpg "https://cli.github.com/packages stable main"
rm -f /etc/apt/keyrings/githubcli-archive-keyring.gpg # former key location

# Node.js (NodeSource's repo, set up without its setup script). Bump
# NODE_MAJOR for a new LTS line. The pin prefers it over Ubuntu's nodejs.
NODE_MAJOR=26
rm -f /etc/apt/sources.list.d/nodesource.sources /usr/share/keyrings/nodesource.gpg # left by the former setup script
repo nodesource https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key "https://deb.nodesource.com/node_${NODE_MAJOR}.x nodistro main"
printf 'Package: nodejs\nPin: origin deb.nodesource.com\nPin-Priority: 600\n' >/etc/apt/preferences.d/nodejs

# Claude Code (Anthropic's repo, "latest" channel). Check the key's
# fingerprint as published in Anthropic's setup docs before trusting it.
CLAUDE_KEY_FPR=31DDDE24DDFAB679F42D7BD2BAA929FF1A7ECACE
curl -fsSL https://downloads.claude.ai/keys/claude-code.asc -o /tmp/claude-code.asc
gpg --show-keys --with-colons /tmp/claude-code.asc | grep -qx "fpr:::::::::${CLAUDE_KEY_FPR}:" \
  || { echo "Claude Code apt key fingerprint mismatch" >&2; exit 1; }
gpg --dearmor --yes -o /etc/apt/keyrings/claude-code.gpg /tmp/claude-code.asc
chmod 644 /etc/apt/keyrings/claude-code.gpg
rm -f /tmp/claude-code.asc
echo "deb [signed-by=/etc/apt/keyrings/claude-code.gpg] https://downloads.claude.ai/claude-code/apt/latest latest main" \
  >/etc/apt/sources.list.d/claude-code.list

# Go from Ubuntu: go.mod files that need a newer Go make it download that
# toolchain itself (GOTOOLCHAIN=auto), verified against sum.golang.org.
apt-get update
apt-get install -y gh nodejs golang-go claude-code
rm -rf /usr/local/go # left by the former go.dev tarball install

# Former per-user installs that would shadow the apt packages on PATH
as_agent 'if [ -L ~/.local/bin/claude ]; then rm -f ~/.local/bin/claude; rm -rf ~/.local/share/claude; fi'

# GitLab CLI: Ubuntu's glab is years behind, and GitLab has no apt repo, so
# take the .deb of the latest release and check it against the release's
# checksums.txt. Tracing is off here: it would dump the release JSON.
glab_install() {
  local rel v deb sums dir
  rel=$(curl -fsSL 'https://gitlab.com/api/v4/projects/gitlab-org%2Fcli/releases/permalink/latest')
  v=$(jq -r '.tag_name | ltrimstr("v")' <<<"$rel")
  if [ "$(dpkg-query -W -f='${Version}' glab 2>/dev/null || true)" = "$v" ]; then
    echo "glab $v is up to date"
    return
  fi
  deb="glab_${v}_linux_amd64.deb"
  dir=$(mktemp -d) && chmod 755 "$dir"
  curl -fsSL -o "$dir/$deb" "$(jq -r --arg n "$deb" '.assets.links[] | select(.name == $n) | .url' <<<"$rel")"
  curl -fsSL -o "$dir/checksums.txt" "$(jq -r '.assets.links[] | select(.name == "checksums.txt") | .url' <<<"$rel")"
  (cd "$dir" && grep -F "  $deb" checksums.txt | sha256sum -c -)
  chmod 644 "$dir/$deb"
  apt-get install -y "$dir/$deb"
  rm -rf "$dir"
}
{ set +x; } 2>/dev/null
glab_install
set -x

# pnpm and Playwright from the npm registry (no current apt packages;
# Ubuntu's Chromium is a snap). Browsers are shared system-wide.
cat >/etc/profile.d/tg-agent.sh <<'EOT'
export PLAYWRIGHT_BROWSERS_PATH=/opt/pw-browsers
export PATH=$PATH:$HOME/go/bin:$HOME/.local/bin
EOT
export PLAYWRIGHT_BROWSERS_PATH=/opt/pw-browsers
npm install -g pnpm playwright
playwright install --with-deps chromium
chmod -R a+rX /opt/pw-browsers
# The browser binaries sit in versioned subdirectories (chromium-<rev>/chrome-linux*/),
# so put the newest Chromium on PATH via a stable symlink instead.
ln -sfn "$(ls -d /opt/pw-browsers/chromium-[0-9]*/chrome-linux*/chrome | sort -V | tail -1)" /usr/local/bin/chromium

# SDKMAN + Java (kept by choice: manages any JDK, Gradle, Maven version)
as_agent '[ -d ~/.sdkman ] || curl -s https://get.sdkman.io | bash'
as_agent 'grep -qxF "source ~/.sdkman/bin/sdkman-init.sh" ~/.profile || echo "source ~/.sdkman/bin/sdkman-init.sh" >> ~/.profile'
as_agent 'source ~/.sdkman/bin/sdkman-init.sh && { [ -e ~/.sdkman/candidates/java/current ] || sdk install java < /dev/null; }'

# uv from PyPI via Ubuntu's pipx (replaces a former install.sh install),
# then Takopi from PyPI via uv
as_agent 'pipx list --short 2>/dev/null | grep -q "^uv " || rm -f ~/.local/bin/uv ~/.local/bin/uvx'
as_agent 'pipx install --force uv'
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
