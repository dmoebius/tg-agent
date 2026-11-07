# tg-agent

Linux CLI that manages **one** libvirt VM (Ubuntu 26.04 LTS), runs Claude Code
inside it and connects the sessions to **one** Telegram bot via
[Takopi](https://github.com/banteg/takopi) (one forum topic per session).

## Commands

| Command | Effect |
|---|---|
| `tg-agent start`  | Creates the VM on first run (download image, cloud-init), starts it, waits for provisioning, clones missing projects |
| `tg-agent stop`   | Shuts the VM down |
| `tg-agent status` | VM state, IP, Takopi service, SSH command |
| `tg-agent update` | Pushes the config into the VM, runs apt upgrade, re-runs the provisioning script (installs new tools, upgrades Claude, Takopi etc.), clones missing projects, restarts Takopi |

## Setup

Prerequisites (checked on start, including install hints): `virsh`, `virt-install`, `qemu-img`, `ssh`, running libvirtd with network `default`, `/dev/kvm`.

1. On first run, `tg-agent start` interactively asks for the bot token, then asks you to post a message mentioning the bot in the forum; from that message it detects the forum chat ID and your Telegram user ID and offers them as defaults (manual entry works too). At least one allowed user ID is required. Everything is stored in `~/.config/tg-agent/config.toml` (0600). Beforehand: create a bot via @BotFather, create a group with Topics enabled, add the bot as admin with "Manage Topics".
2. It then creates the VM (takes a few minutes the first time) and interactively runs `claude auth login --claudeai` in the VM (Claude subscription).

The VM is persistent: stopping, starting and updating keep everything in it, including `~/work`. If the libvirt domain disappears, `tg-agent start` redefines it on top of the existing disk.

### Projects and topics

Takopi runs in topic mode: every forum topic is bound to one project and branch and keeps its own Claude session, which survives VM restarts. Declare the projects in `config.toml`:

```toml
[[projects]]
alias = "tg-agent"                                   # used in /topic
url = "git@github.com:dmoebius/tg-agent.git"
worktree_base = "main"                               # optional: base branch for new worktrees
```

`tg-agent start` / `tg-agent update` clone missing projects into `~/work/<alias>` (existing checkouts are never touched) and register them with Takopi; each `@branch` gets its own worktree under `~/work/<alias>/.worktrees/`. Private repositories need git credentials in the VM (e.g. an SSH key or `gh auth login` via the SSH command from `tg-agent status`). The project list is owned by `config.toml`: projects added in the VM with `takopi init` are overwritten by the next `tg-agent update`.

In a topic, bind it with `/topic <alias> @<branch>`; `/ctx` shows the binding, `/new` resets the topic's session.

Inside the VM (user `agent`, passwordless sudo, so sessions can install packages themselves): Java + SDKMAN, Node LTS + pnpm, Playwright (Chromium), Go, Python + uv, Claude Code, Takopi (systemd service `takopi`, working directory `~/work`).

Note: with `qemu:///system` the qemu user needs read access to `data_dir` (default `~/.local/share/tg-agent`); if necessary, move `data_dir` to `/var/lib/libvirt/images/tg-agent`.

## Development

    make build && make test

## Claude skills

The skills from [mattpocock/skills](https://github.com/mattpocock/skills) are provided as a plugin (`mattpocock-skills@mattpocock`), configured in `.claude/settings.json`. Claude Code offers to install it when the repo is opened and keeps the plugin up to date. The repo configuration for the skills lives in `CLAUDE.md` and `docs/agents/`.
