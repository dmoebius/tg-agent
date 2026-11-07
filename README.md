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
| `tg-agent update` | Merges the Telegram settings into Takopi's config, re-runs the provisioning script (apt upgrade, installs new tools, upgrades Claude, Takopi etc.), restarts Takopi |

## Setup

Prerequisites (checked on start, including install hints): `virsh`, `virt-install`, `qemu-img`, `ssh`, running libvirtd with network `default`, `/dev/kvm`.

1. On first run, `tg-agent start` interactively asks for the bot token, then asks you to post a message mentioning the bot in the forum; from that message it detects the forum chat ID and your Telegram user ID and offers them as defaults (manual entry works too). At least one allowed user ID is required. Everything is stored in `~/.config/tg-agent/config.toml` (0600). Beforehand: create a bot via @BotFather and a group with Topics enabled. The setup then prints a link (`https://t.me/<bot>?startgroup=true&admin=manage_topics`) that adds the bot to the group as admin with just "Manage Topics" (a bot cannot promote itself). `start` and `update` verify this right, because Takopi refuses to run without it; interactively they wait until it is granted.
2. It then creates the VM (takes a few minutes the first time) and interactively runs `claude auth login --claudeai` in the VM (Claude subscription).

The VM is persistent: stopping, starting and updating keep everything in it, including `~/work`. If the libvirt domain disappears, `tg-agent start` redefines it on top of the existing disk.

### Projects and topics

Takopi runs in topic mode: every forum topic is bound to one project and branch and keeps its own Claude session, which survives VM restarts.

Projects are registered with Takopi inside the VM, not in `config.toml`. Either do it via SSH (`tg-agent status` prints the command) or simply ask Claude in any topic:

    git clone git@github.com:you/foo.git ~/work/foo
    cd ~/work/foo && takopi init foo

`takopi init` uses `.worktrees` for branch worktrees and the repo's default branch as their base. Takopi picks up the change immediately (`watch_config`). Private repositories need git credentials in the VM: log in once with `gh auth login` / `glab auth login` or add an SSH key.

`tg-agent update` only merges its own keys (bot token, chat ID, allowed users, topic mode) into `~/.takopi/takopi.toml`; projects and other Takopi settings are kept.

Send `/topic <alias> @<branch>` in the forum: Takopi creates a new topic named after it and binds it. A topic you create by hand in Telegram stays unbound until you send `/ctx set <alias> @<branch>` in it. `/ctx` shows the binding, `/new` resets the topic's session.

Inside the VM (user `agent`, passwordless sudo, so sessions can install packages themselves): GitHub CLI `gh`, GitLab CLI `glab`, Java + SDKMAN, Node LTS + pnpm, Playwright (Chromium, also on `PATH` as `chromium`), Go, Python + uv, Claude Code, Takopi (systemd service `takopi`, working directory `~/work`).

Note: with `qemu:///system` the qemu user (`libvirt-qemu` or `qemu`) must be able to reach the disk in `data_dir` (default `~/.local/share/tg-agent`). `tg-agent start` therefore adds a search-only ACL (`setfacl -m u:libvirt-qemu:x`) to `data_dir` and to each of its parent directories that you own and that others cannot enter, like virt-manager does. If you do not want that, set `data_dir` to e.g. `/var/lib/libvirt/images/tg-agent`.

## Development

    make build && make test

## Claude skills

The skills from [mattpocock/skills](https://github.com/mattpocock/skills) are provided as a plugin (`mattpocock-skills@mattpocock`), configured in `.claude/settings.json`. Claude Code offers to install it when the repo is opened and keeps the plugin up to date. The repo configuration for the skills lives in `CLAUDE.md` and `docs/agents/`.
