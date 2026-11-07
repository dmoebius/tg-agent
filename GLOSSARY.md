# tg-agent

tg-agent runs Claude Code inside one long-lived VM on the user's machine and lets the user drive it from Telegram, one forum topic per piece of work.

## Infrastructure

**Host**:
The user's own Linux machine on which the `tg-agent` CLI runs and which hosts the VM.
_Avoid_: server, local machine

**VM**:
The single, persistent virtual machine in which all agents work; it and everything on it survive restarts and updates.
_Avoid_: guest, domain, box, sandbox

**Config**:
The user's settings for tg-agent, kept on the Host; the source of truth for everything tg-agent sets up in the VM.
_Avoid_: settings, takopi config

**Provisioning**:
Bringing the VM's installed tooling and services to the state tg-agent defines; repeatable at any time.
_Avoid_: setup, bootstrap, install

**Update**:
Pushing the current Config into the VM, re-running Provisioning and upgrading all installed tooling to its latest version.
_Avoid_: upgrade, sync, refresh

## Telegram

**Bot**:
The one Telegram bot through which the user talks to the agents in the VM.
_Avoid_: Takopi (that is the software behind the bot)

**Forum**:
The one Telegram supergroup with topics enabled in which the Bot operates.
_Avoid_: group, chat

**Topic**:
A thread inside the Forum; each Topic is bound to exactly one Project and Branch and carries exactly one Session.
_Avoid_: thread, channel, chat

**Allowed User**:
A Telegram user permitted to control the agents through the Bot; at least one is required.
_Avoid_: admin, member, whitelist entry

## Work

**Workspace**:
The persistent area in the VM that holds all Projects.
_Avoid_: work dir, home

**Project**:
A git repository declared in the Config under a short alias, which tg-agent keeps checked out in the Workspace so Topics can be bound to it.
_Avoid_: repo, alias

**Binding**:
The association of a Topic with a Project and Branch, which fixes where that Topic's Session works.
_Avoid_: context, ctx

**Session**:
One ongoing Claude conversation belonging to a Topic; it survives VM restarts and is only discarded on explicit reset.
_Avoid_: chat, conversation, run

**Agent**:
Claude as it works inside a Session.
_Avoid_: bot, assistant
