# tg-agent

Linux-CLI, das **eine** libvirt-VM (Ubuntu 26.04 LTS) verwaltet, darin Claude Code
startet und die Sessions über [Takopi](https://github.com/banteg/takopi) mit
**einem** Telegram-Bot verbindet (ein Forum-Topic pro Session).

## Befehle

| Befehl | Wirkung |
|---|---|
| `tg-agent start`  | Legt die VM beim ersten Mal an (Image laden, cloud-init), startet sie, wartet auf Provisionierung |
| `tg-agent stop`   | Fährt die VM herunter |
| `tg-agent status` | VM-Zustand, IP, Takopi-Service, SSH-Kommando |
| `tg-agent update` | Config in die VM pushen, apt upgrade, Claude + Takopi updaten, Takopi neu starten |

## Setup

Voraussetzungen: `libvirt` (laufender libvirtd, Netzwerk `default`), `virt-install`, `virsh`, `qemu-img`, `ssh`.

1. `tg-agent start` schreibt beim ersten Aufruf `~/.config/tg-agent/config.toml` (Template) und bricht ab.
2. Telegram: Supergroup mit aktivierten Topics anlegen, Bot als Admin mit „Manage Topics“ hinzufügen, `bot_token` und `chat_id` eintragen (`allowed_user_ids` setzen!).
3. `tg-agent start` erneut ausführen. Das dauert beim ersten Mal einige Minuten.
4. Ohne `claude.api_key`: einmal per SSH einloggen und `claude` zum Anmelden starten.

In der VM (User `agent`): Java + SDKMAN, Node LTS + pnpm, Playwright (Chromium), Go, Python + uv, Claude Code, Takopi (systemd-Service `takopi`, Arbeitsverzeichnis `~/work`).

Hinweis: Bei `qemu:///system` braucht der qemu-User Leserechte auf `data_dir` (Default `~/.local/share/tg-agent`), ggf. `data_dir` nach `/var/lib/libvirt/images/tg-agent` legen.

## Entwicklung

    make build && make test
