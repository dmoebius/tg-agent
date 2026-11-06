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

Voraussetzungen (werden beim Start geprüft, inkl. Installationshinweisen): `virsh`, `virt-install`, `qemu-img`, `ssh`, laufender libvirtd mit Netzwerk `default`, `/dev/kvm`.

1. `tg-agent start` fragt beim ersten Aufruf interaktiv nach Bot-Token, Gruppen-Chat-ID und erlaubten Telegram-User-IDs und speichert sie in `~/.config/tg-agent/config.toml` (0600). Vorher: Bot bei @BotFather anlegen, Gruppe mit aktivierten Topics erstellen, Bot als Admin mit „Manage Topics“ hinzufügen.
2. Danach legt es die VM an (dauert beim ersten Mal einige Minuten) und startet interaktiv `claude auth login --claudeai` in der VM (Claude-Subscription).

In der VM (User `agent`): Java + SDKMAN, Node LTS + pnpm, Playwright (Chromium), Go, Python + uv, Claude Code, Takopi (systemd-Service `takopi`, Arbeitsverzeichnis `~/work`).

Hinweis: Bei `qemu:///system` braucht der qemu-User Leserechte auf `data_dir` (Default `~/.local/share/tg-agent`), ggf. `data_dir` nach `/var/lib/libvirt/images/tg-agent` legen.

## Entwicklung

    make build && make test

## Claude-Skills

Die Skills von [mattpocock/skills](https://github.com/mattpocock/skills) kommen als Plugin (`mattpocock-skills@mattpocock`), konfiguriert in `.claude/settings.json`. Claude Code bietet beim Öffnen des Repos die Installation an und hält das Plugin aktuell. Die Repo-Konfiguration für die Skills steht in `AGENTS.md` und `docs/agents/`.

Hinweis: Claude Code liest `CLAUDE.md`, nicht `AGENTS.md`. Falls der Skills-Block nicht greift, `CLAUDE.md` mit dem Inhalt `@AGENTS.md` anlegen.
