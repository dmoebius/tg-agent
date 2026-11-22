# tg-agent

Go CLI that manages a single libvirt VM running Claude Code and Takopi (Telegram bot, one topic per session). See `README.md`. Build/test: `make build`, `make test`.

**Project language is English**: docs, code, identifiers, strings, comments, commit messages. This applies even when the chat with the user is in German.

## Git workflow

- Before every commit, run `gofmt -w` on all changed Go files.
- Simple changes: commit and push directly to `main`.
- Complex changes: feature branch with prefix `feat/`.
- Fixes: branch with prefix `fix/`.
- When in doubt, ask the user.

## Agent skills

### Issue tracker

Issues live in GitHub Issues of `dmoebius/tg-agent` (use the `gh` CLI). See `docs/agents/issue-tracker.md`.

### Triage labels

Default vocabulary: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `GLOSSARY.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.

Glossary terms (Host, Config, Session, ...) are capitalized only in specs, docs, issues and code comments. In CLI output (help texts, flag usage, messages, errors, prompts) they are ordinary words in lower case: "the host", "the config", "running sessions". Sentence starts, field labels like `Config:` in `status`, proper nouns (Telegram, Takopi, Claude) and Telegram UI names ("Manage Topics") keep their capitals.
