---
status: superseded by ADR-0004
---

# Projects are declared in the Host Config, not registered in the VM

Takopi normally learns about Projects through `takopi init` inside the VM, which writes them into its own `takopi.toml`. tg-agent instead owns that file completely: Projects are declared in the Config on the Host, cloned into the Workspace and rendered into `takopi.toml` on every Update. This keeps the Config the single source of truth and makes the VM reproducible from it; merging hand-made VM-side entries with generated ones was rejected as fragile. Consequence: Projects registered with `takopi init` in the VM are overwritten by the next Update.
