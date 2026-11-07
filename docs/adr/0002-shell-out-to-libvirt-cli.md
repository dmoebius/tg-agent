# Drive libvirt via its CLI tools instead of Go bindings

The VM is managed by shelling out to `virsh`, `virt-install` and `qemu-img` rather than using the libvirt Go bindings. The bindings require cgo and libvirt development headers at build time, whereas the CLI tools are already required on any libvirt Host; `virt-install --cloud-init` also gives us cloud-init support for free. The cost is parsing human-readable command output (e.g. `domstate`, `domifaddr`).
