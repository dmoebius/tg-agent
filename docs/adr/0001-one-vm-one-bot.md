# Exactly one VM and one Bot per Host

tg-agent deliberately manages a single VM and a single Telegram Bot; parallel work is separated by Topics bound to different Projects/Branches inside that VM, not by additional VMs. This keeps the CLI free of instance naming, selection and per-instance state. Supporting multiple VMs would change the Config format, the CLI surface and the on-disk layout, so it should not be added casually.
