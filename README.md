# tg-agent

Linux CLI that manages **one** libvirt VM (Ubuntu 26.04 LTS), runs Claude Code
inside it and connects the sessions to **one** Telegram bot via
[Takopi](https://github.com/banteg/takopi) (one forum topic per session).

## Commands

| Command | Effect |
|---|---|
| `tg-agent start`  | Creates the VM on first run (download image, cloud-init), starts it, waits for provisioning |
| `tg-agent stop`   | Shuts the VM down |
| `tg-agent status` | VM state, IP, Takopi service, SSH command |
| `tg-agent update` | Pushes the config into the VM, runs apt upgrade, updates Claude + Takopi, restarts Takopi |

## Setup

Prerequisites (checked on start, including install hints): `virsh`, `virt-install`, `qemu-img`, `ssh`, running libvirtd with network `default`, `/dev/kvm`.

1. On first run, `tg-agent start` interactively asks for the bot token, group chat ID and allowed Telegram user IDs and stores them in `~/.config/tg-agent/config.toml` (0600). Beforehand: create a bot via @BotFather, create a group with Topics enabled, add the bot as admin with "Manage Topics".
2. It then creates the VM (takes a few minutes the first time) and interactively runs `claude auth login --claudeai` in the VM (Claude subscription).

Inside the VM (user `agent`): Java + SDKMAN, Node LTS + pnpm, Playwright (Chromium), Go, Python + uv, Claude Code, Takopi (systemd service `takopi`, working directory `~/work`).

Note: with `qemu:///system` the qemu user needs read access to `data_dir` (default `~/.local/share/tg-agent`); if necessary, move `data_dir` to `/var/lib/libvirt/images/tg-agent`.

## Development

    make build && make test

## Claude skills

The skills from [mattpocock/skills](https://github.com/mattpocock/skills) are provided as a plugin (`mattpocock-skills@mattpocock`), configured in `.claude/settings.json`. Claude Code offers to install it when the repo is opened and keeps the plugin up to date. The repo configuration for the skills lives in `CLAUDE.md` and `docs/agents/`.
