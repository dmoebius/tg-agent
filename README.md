# tg-agent

Linux CLI that manages **one** libvirt VM (Ubuntu 26.04 LTS), runs Claude Code
inside it and connects the sessions to **one** Telegram bot via
[Takopi](https://github.com/banteg/takopi) (one forum topic per session).

## Commands

- `tg-agent start`: Creates the VM on first run (download image, cloud-init), starts it, waits for provisioning. When it creates the VM (first run, or after `delete`) it asks for CPUs, memory and disk size, offering the Config's values, and saves the answers; without a terminal it uses the Config's values. Starting an existing VM does not ask.
- `tg-agent stop`: Shuts the VM down
- `tg-agent restart`: Shuts the running VM down and starts it again, like `stop` followed by `start` (running Sessions are interrupted). Fails if the VM is not running.
- `tg-agent status`: Config file and `data_dir` paths, Forum link, VM state, CPUs, memory and disk size (flagging values pending a restart or differing from the Config), IP, Takopi service, SSH command
- `tg-agent doctor [--fix]`: Finds out why the bot does not answer. `status` only shows whether the Takopi service runs; `doctor` checks the whole path: bot token and the bot's rights in the forum, a webhook blocking polling, VM state and SSH, the VM's network step by step (Host, internet, DNS, `api.telegram.org`), the Takopi service, whether its config in the VM matches `config.toml`, Claude's login and errors in Takopi's log. Without flags it only reports. `--fix` also repairs what it can: starts a stopped VM, removes a webhook, lets the VM's traffic through Docker's firewall on the Host (asks for your sudo password; every sudo command is printed before it runs) and restarts Takopi. Exits with status 1 if problems remain.
- `tg-agent update [--cpus N] [--memory SIZE] [--disk SIZE]`: Writes the flags to the Config, then brings the running VM in line with the Config: grows the disk, sets CPUs and memory, merges the Telegram settings into Takopi's config, re-runs the provisioning script (apt upgrade, installs new tools, upgrades Claude, Takopi etc.), restarts Takopi. Values changed by hand in `config.toml` are applied too. `SIZE` takes units (`16G`, `16384M`); a plain number means GiB. The disk only grows, and the VM uses the new space at once. New CPUs and memory need a restart of the VM: `update` shows old and new values and asks before restarting (running Sessions are interrupted); without a terminal it refuses, and the change takes effect on the next `restart` (or `stop` and `start`).
- `tg-agent shell` (aliases `sh`, `ssh`) `[<command> [<args>...]]`: Without arguments: login shell in the VM as `agent`. With arguments: runs the command in a login shell in the VM (stdin/stdout/stderr passed through, TTY only if stdin is a terminal) and exits with its exit code. Arguments are passed verbatim, so remote `~` or `$VAR` must go through a shell: `tg-agent sh bash -c 'cat ~/.takopi/takopi.toml'`
- `tg-agent delete [--wipe-disk | --wipe-all]`: Start over. `delete` shuts the VM down (forced off after 30 s) and removes it from libvirt (`virsh undefine`); disk, `data_dir` and config are kept, so `start` re-creates the VM on the existing disk. `--wipe-disk` also removes `data_dir` (disk, base image, SSH key, cloud-init files) and the libvirt storage pool pointing at it: all Projects and Sessions are lost. `--wipe-all` also removes the whole config directory (`~/.config/tg-agent`, including the bot token), so `start` runs the first-run setup again. Shows what will be destroyed and asks you to type the VM name; refuses without a terminal. Missing parts are skipped, so it also cleans up after a half-failed `start`. The search ACLs added by `start` are kept.

## Setup

Prerequisites (checked on start, including install hints): `virsh`, `virt-install`, `qemu-img`, `ssh`, running libvirtd with network `default`, `/dev/kvm`.

1. On first run, `tg-agent start` interactively asks for the bot token, then asks you to post a message mentioning the bot in the forum; from that message it detects the forum chat ID and your Telegram user ID and offers them as defaults (manual entry works too). At least one allowed user ID is required. Everything is stored in `~/.config/tg-agent/config.toml` (0600). Beforehand: create a bot via @BotFather and a group with Topics enabled. The setup then prints a link (`https://t.me/<bot>?startgroup=true&admin=manage_topics`) that adds the bot to the group as admin with just "Manage Topics" (a bot cannot promote itself). `start` and `update` verify this right, because Takopi refuses to run without it; interactively they wait until it is granted.
2. It then creates the VM (takes a few minutes the first time) and interactively runs `claude auth login --claudeai` in the VM (Claude subscription).

VM resources default to half of the Host's CPUs, 8 GiB memory and a 60 GiB disk (`cpus`, `memory_mb`, `disk_gb` in `[vm]` of `config.toml`). Limits: 1 up to the Host's CPUs, at least 2 GiB memory (warning above 75 % of the Host's), at least 20 GiB disk.

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

Provisioning installs from signed apt repositories wherever possible and does not pipe downloaded install scripts into a shell:

- From Ubuntu: Go (newer toolchains required by a `go.mod` are downloaded and checksum-verified by Go itself), Python, pipx.
- From the vendors' apt repos: `gh` (GitHub), Node.js (NodeSource), and Claude Code (Anthropic, `latest` channel, key fingerprint checked). These are updated by `apt upgrade` during `tg-agent update`, not by `claude update`.
- `glab` comes as a release `.deb` checked against the release's `checksums.txt`, because GitLab has no apt repo and Ubuntu's version is very old.
- pnpm and Playwright come from npm, uv and Takopi from PyPI.
- The one exception is SDKMAN, which is still installed via its install script.

Note: with `qemu:///system` the qemu user (`libvirt-qemu` or `qemu`) must be able to reach the disk in `data_dir` (default `~/.local/share/tg-agent`). `tg-agent start` therefore adds a search-only ACL (`setfacl -m u:libvirt-qemu:x`) to `data_dir` and to each of its parent directories that you own and that others cannot enter, like virt-manager does. If you do not want that, set `data_dir` to e.g. `/var/lib/libvirt/images/tg-agent`.

### Docker on the Host

If Docker runs on the Host, the VM may have no internet: the Takopi service is "active" but the bot never answers. Docker sets the policy of the iptables `FORWARD` chain to DROP and only accepts its own networks; libvirt's nftables rules live in a separate table and cannot override that. `tg-agent doctor --fix` adds accept rules for the VM's bridge to Docker's `DOCKER-USER` chain. They last until the Host reboots. For a permanent solution set `firewall_backend = "iptables"` in `/etc/libvirt/network.conf` and restart libvirtd, so libvirt puts its rules into the same chain as Docker.

## Development

    make build && make test

## Claude skills

The skills from [mattpocock/skills](https://github.com/mattpocock/skills) are provided as a plugin (`mattpocock-skills@mattpocock`), configured in `.claude/settings.json`. Claude Code offers to install it when the repo is opened and keeps the plugin up to date. The repo configuration for the skills lives in `CLAUDE.md` and `docs/agents/`.
