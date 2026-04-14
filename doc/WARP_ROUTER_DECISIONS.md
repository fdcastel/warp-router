# Warp Router — Decisions & Lessons Learned

> **Keep this document always updated.** After every significant decision, workaround, or lesson learned during development, add an entry here with the date, context, and rationale. This serves as the project's institutional memory — when someone asks "why did we do X?", the answer should be here.

---

## How to Use This Document

1. **New decision**: Add a new `###` section under the appropriate category. Include the date, what was decided, why, and what alternatives were considered.
2. **Lesson learned**: Add under "Lessons Learned" with the date and what went wrong or what we discovered.
3. **Reversal**: Don't delete old entries. Add a new entry referencing the old one and explain why the decision changed.
4. **Review**: Skim this document at the start of each work session to avoid repeating past mistakes.

---

## Architecture Decisions

### AD-001: Debian 13 (Trixie) as Base OS (2026-04-12)
**Decision**: Use Debian 13 (Trixie) minimal as the base image OS.  
**Rationale**: Modern kernel (6.x), FRR 10.x packages available in repos, long upstream support cycle. Ubuntu was considered but Debian's minimal install is smaller and more predictable for appliance use.

### AD-002: XDP/eBPF Removed from v1 Scope (2026-04-12)
**Decision**: Drop XDP/eBPF fast-path filtering from v1.  
**Rationale**: Adds significant complexity (custom eBPF programs, kernel compat matrix, separate datapath). Standard Linux forwarding with nftables is sufficient for the initial release. Revisit in v2 once the control plane is stable.

### AD-003: FRR for All Routing (2026-04-12)
**Decision**: Use FRR (Free Range Routing) for ECMP, PBR, static routes, and BFD.  
**Rationale**: Industry-standard Linux routing daemon. Supports everything we need (BGP, ECMP, PBR, BFD) in one package. Alternatives like BIRD lack PBR support.

### AD-004: Kea over ISC DHCP (2026-04-12)
**Decision**: Use ISC Kea for DHCP.  
**Rationale**: ISC DHCP is deprecated. Kea is its official successor with JSON-based config (easy to template from Go) and modern lease management.

### AD-005: SSH + pct Instead of Proxmox REST API (2026-04-13)
**Decision**: Replace the Proxmox REST API (`go-proxmox`) with direct SSH commands to the PVE host using `pct`/`qm` CLI tools.  
**Rationale**: The `go-proxmox` library targets Proxmox API v2, but LXC provisioning via the API is fragile — container creation is async, status polling is unreliable, and SDN zone APIs require PVE 8.1+ with specific cluster config. Direct SSH to the PVE host and running `pct create`, `pct start`, `pct exec` is far simpler, synchronous, and doesn't require API tokens or TLS setup. The test framework runs from a dev LXC (tst54) that already has SSH access to the PVE host.  
**Alternatives rejected**: `go-proxmox` (fragile async API, SDN issues), Terraform (overkill for ephemeral test topologies).

### AD-006: Ephemeral Linux Bridges Instead of Proxmox SDN Zones (2026-04-13)
**Decision**: Use ephemeral Linux bridges (`ip link add ... type bridge`) for test network isolation instead of Proxmox SDN zones.  
**Rationale**: Proxmox SDN requires zone configuration, VNET creation, and subnet allocation — all via API calls that are slow and version-dependent. Simple Linux bridges created via SSH on the PVE host provide the same L2 isolation with zero dependencies. Each test run creates a uniquely-named bridge (e.g., `wt858b43`), attaches containers, and destroys it on teardown.

### AD-007: Separate Go Module for Tests (2026-04-13)
**Decision**: Put integration tests under `test/` with their own `go.mod`.  
**Rationale**: Integration tests depend on `x/crypto/ssh` and the PVE provisioning framework, which are not needed by the main `warp` binary. A separate module keeps the main binary's dependency tree clean and prevents test-only code from being compiled into production.

### AD-008: ifupdown Instead of systemd-networkd for LXC (2026-04-14)
**Decision**: Install `ifupdown` in the rootfs and disable `systemd-networkd`.  
**Rationale**: Proxmox writes `/etc/network/interfaces` when configuring LXC container networking (via `--net0 ip=...,gw=...`). This file format is only understood by `ifupdown`, not `systemd-networkd`. Without `ifupdown`, containers start with no network. For QCOW2 VMs, cloud-init handles networking separately, so this doesn't conflict.

### AD-009: Unbound Forwarding Mode Disables DNSSEC Validator (2026-04-14)
**Decision**: When DNS forwarders are configured, render `module-config: "iterator"` (no validator module) in unbound.conf.  
**Rationale**: The DNSSEC validator module fails to initialize in unprivileged LXC containers — it can't write to `/var/lib/unbound/root.key` reliably or the trust anchor update process fails. Since forwarding mode delegates resolution to upstream resolvers (which handle DNSSEC themselves), disabling the local validator is semantically correct. Full recursion mode (no forwarders) still enables DNSSEC with `auto-trust-anchor-file`.

### AD-010: sysctl Uses -e Flag for Graceful Degradation (2026-04-14)
**Decision**: Run `sysctl -e -p` instead of `sysctl -p` in the apply pipeline.  
**Rationale**: In unprivileged LXC containers, certain sysctl keys (notably `net.netfilter.nf_conntrack_max`) are blocked by the kernel. Without `-e`, the entire sysctl apply step fails, which blocks the rest of the pipeline (FRR, nftables, etc.). The `-e` flag makes sysctl ignore individual errors while still applying everything it can.

### AD-011: FRR Requires frr-pythontools Package (2026-04-14)
**Decision**: Add `frr-pythontools` to the rootfs package list.  
**Rationale**: FRR's `frrinit.sh reload` command (invoked by `systemctl reload frr`) requires the `frr-pythontools` package to perform live config reloads. Without it, every reload attempt fails with "The frr-pythontools package is required for reload functionality." This was discovered during integration testing when `warp apply` succeeded in writing the config but failed on the reload step.

### AD-012: QCOW2 Build Deferred — Requires Bare Metal or Privileged VM (2026-04-14)
**Decision**: Defer QCOW2 image testing (task 2.5) until a suitable build environment is available.  
**Rationale**: The QCOW2 build script requires `losetup`, `sgdisk`, `mkfs.ext4`, `mount`, `chroot`, and `grub-install` — all of which need either bare-metal access or a privileged VM. The dev environment (tst54) is an unprivileged LXC with no loop devices, no block device access, and seccomp restrictions. See `tmp/CURRENT_LIMITATIONS.md` for details and alternatives.

---

## Lessons Learned

### LL-001: mmdebstrap Fails in LXC Due to seccomp (2026-04-12)
**Context**: Tried to build rootfs inside the tst54 dev LXC.  
**Problem**: `mmdebstrap` uses `chroot` internally, which calls socket() — blocked by LXC's seccomp profile.  
**Resolution**: Build rootfs on the PVE host (bare-metal Debian) instead. Rsync project files to the host, run `make rootfs` there, copy the template back.

### LL-002: Proxmox API is Fragile for Test Automation (2026-04-13)
**Context**: Initially built test infrastructure using `go-proxmox` REST client.  
**Problem**: Container creation is async (returns a task ID), status transitions are delayed, and SDN zone APIs have version-specific quirks.  
**Resolution**: Replaced with SSH + `pct` commands. Synchronous, predictable, and the PVE host is always accessible. See AD-005.

### LL-003: Container Networking Requires ifupdown (2026-04-14)
**Context**: First LXC container from warp-router template had no network connectivity.  
**Problem**: Proxmox writes `/etc/network/interfaces` but the rootfs had `systemd-networkd` (no `ifupdown`).  
**Resolution**: Added `ifupdown` to packages, disabled `systemd-networkd`. See AD-008.

### LL-004: FRR Reload is Not a Simple systemctl restart (2026-04-14)
**Context**: `warp apply` wrote the FRR config correctly but `systemctl reload-or-restart frr` failed.  
**Problem**: FRR's reload mechanism uses Python scripts (`/usr/lib/frr/frr-reload.py`) from the `frr-pythontools` package, which wasn't installed.  
**Resolution**: Added `frr-pythontools` to packages.list. See AD-011.

### LL-005: DNSSEC Validator Fails in Unprivileged LXC (2026-04-14)
**Context**: Unbound crashed on startup after `warp apply` in test containers.  
**Problem**: The DNSSEC validator module couldn't initialize — likely due to restricted filesystem access to `/var/lib/unbound/root.key` in unprivileged LXC.  
**Resolution**: Disable validator in forwarding mode (`module-config: "iterator"`). See AD-009.

### LL-006: Stale Containers Cause VMID Collisions (2026-04-14)
**Context**: Integration test suite failed when run multiple times.  
**Problem**: If a test fails mid-run, containers aren't cleaned up. Next run tries to create containers with the same VMIDs, gets "CT already exists."  
**Resolution**: Added pre-test cleanup (`pve.DestroyCT(vmid)` at start of each test) and improved `StopCT` to wait for actual stop before destroying. `t.Cleanup()` handles normal teardown.

### LL-007: Unbound Can't Bind to Non-Existent IPs (2026-04-14)
**Context**: Integration tests used `dummy0` as LAN device with IP 192.168.99.1.  
**Problem**: The unbound renderer auto-adds LAN IPs as listen interfaces, but `dummy0` doesn't actually exist in the container, so unbound can't bind to 192.168.99.1.  
**Resolution**: Tests that use dummy interfaces explicitly set `dns.listen: [127.0.0.1]` to override the auto-generated listen list.

### LL-008: Proxmox-Automation Scripts Use a Different Approach (2026-04-14)
**Context**: Investigated whether `tmp/Proxmox-Automation/new-vm.sh` could help build QCOW2 images.  
**Finding**: The scripts don't build images — they consume pre-built cloud images (e.g., Debian genericcloud QCOW2 from upstream). `new-vm.sh` uses `qm create --scsi0 import-from=<image>` to import an existing QCOW2 as a VM disk, then configures cloud-init for first-boot customization. This is an orthogonal approach: we need to *build* the QCOW2, not just import one.  
**Potential use**: Could adapt the `new-vm.sh` approach for integration testing — use our LXC template as the primary image and run VM tests by importing a generic cloud image + cloud-init warp configuration.
