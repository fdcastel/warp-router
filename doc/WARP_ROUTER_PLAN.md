# Warp Router — Implementation Plan

> **Created**: 2026-04-12
> **Repository**: https://github.com/fdcastel/warp-router
> **Base Image**: Debian 13 (Trixie) minimal, x86_64
> **Language**: Go 1.26 (control plane, CLI, test harness)

---

## Plan Maintenance Instructions

**This document is the single source of truth for project progress. Keep it updated as work progresses.**

1. **Before starting a task**: Change its status from ❌ OPEN to 🔧 IN PROGRESS.
2. **After completing a task**: Change its status to ✅ RESOLVED. Add a one-line note with the commit or PR reference if applicable.
3. **If a task is blocked**: Change its status to ⏯️ DEFERRED and add a note explaining the blocker and which task must be resolved first.
4. **Adding new tasks**: Insert them at the end of the appropriate phase. Assign the next available task number within that phase.
5. **Phase gates**: Do not begin a new phase until all non-deferred tasks in the previous phase are ✅ RESOLVED.
6. **Review cadence**: Review this plan at the start of each work session and after completing any phase.

### Status Legend

| Icon | Meaning |
|------|---------|
| ✅ | RESOLVED — Implemented and tested |
| 🔧 | IN PROGRESS — Partially implemented or underway |
| ❌ | OPEN — Not yet addressed |
| ⏯️ | DEFERRED — Delayed until a blocking task is finished |

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────┐
│                    Warp Router                          │
│                                                         │
│  ┌──────────┐  ┌──────────┐  ┌───────┐  ┌───────────┐   │
│  │   FRR    │  │ nftables │  │  Kea  │  │ Unbound   │   │
│  │ (BGP/    │  │ (FW/NAT/ │  │(DHCPv4│  │ (DNS      │   │
│  │  ECMP/   │  │  conntrk)│  │server)│  │  resolver)│   │
│  │  PBR)    │  │          │  │       │  │           │   │
│  └────┬─────┘  └────┬─────┘  └───┬───┘  └─────┬─────┘   │
│       │              │            │             │       │
│  ┌────▼──────────────▼────────────▼─────────────▼────┐  │
│  │        Linux Kernel Networking Stack              │  │
│  │   (routing table, policy rules, conntrack,        │  │
│  │    ip forwarding, multiqueue NICs)                │  │
│  └───────────────────┬───────────────────────────────┘  │
│                      │                                  │
│       ┌──────────────┼──────────────┐                   │
│     WAN1           WAN2           LAN                   │
└─────────────────────────────────────────────────────────┘

Build Pipeline:
  mmdebstrap ──► rootfs ──┬──► tar.zst (LXC template)
                          └──► virt-customize ──► qcow2 (VM + cloud-init)

Test Pipeline (Go):
  go test ──► Proxmox API ──► provision topology ──► SSH tests ──► teardown
```

### Key Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Base OS | Debian 13 (Trixie) minimal | Modern kernel (6.x), FRR packages available, long support |
| XDP/eBPF | **Removed from v1** | Too complex for initial release; revisit in v2 |
| Image build (LXC) | mmdebstrap + tar --zstd | Simple, reproducible, no Packer dependency |
| Image build (QCOW2) | mmdebstrap + virt-customize | Shared rootfs pipeline, cloud-init pre-installed |
| DHCP server | Kea (ISC) | Robust, JSON config, modern replacement for ISC DHCP |
| DNS resolver | Unbound | Full recursive, proven in router appliances |
| Routing suite | FRR | ECMP, PBR, BGP, BFD — industry standard for Linux routers |
| Firewall/NAT | nftables | Native Linux, replaces iptables, atomic rule loading |
| Control plane language | Go 1.26 | Single binary, strong networking libs, fast test runner |
| Proxmox client (tests) | luthermonson/go-proxmox | Typed SDK, less boilerplate than raw HTTP |
| Test network isolation | Proxmox SDN zones | Clean per-run isolation, no bridge name collisions |
| FRR config management | Static frr.conf generation | Simple, auditable, reload via `systemctl reload frr` |
| CI runner (build) | GitHub-hosted runners | Standard, no infrastructure to maintain |
| CI runner (integration) | Separate trigger (workflow_dispatch) | Proxmox tests triggered on-demand or on release tags |
| Release artifacts | GitHub Releases | LXC .tar.zst + QCOW2 .qcow2 per version tag |

---

## Project Structure

```text
cmd/
└── warp/                     # CLI entrypoint (Phase 3)
    └── main.go

internal/
├── config/                   # YAML site config model + validation
├── apply/                    # Transactional apply pipeline
├── services/                 # FRR, nftables, Kea, Unbound adapters
│   ├── frr/
│   ├── nftables/
│   ├── kea/
│   └── unbound/
├── health/                   # WAN health checks, probe logic
├── revisions/                # Config revision store (current + last-known-good)
└── observability/            # CLI status/inspect commands

packaging/
├── rootfs/                   # mmdebstrap config, package lists, overlay files
│   ├── packages.list         # APT packages to install
│   ├── overlay/              # Files copied into rootfs (configs, systemd units)
│   └── hooks/                # mmdebstrap customize hooks
├── lxc/
│   └── build-lxc.sh          # rootfs → tar.zst LXC template
├── qcow2/
│   └── build-qcow2.sh        # rootfs → qcow2 via virt-customize + cloud-init
└── cloud-init/
    └── seed/                  # Default cloud-init NoCloud templates

test/
├── integration/               # Go test suite — Proxmox orchestration + SSH tests
│   ├── testenv/               # Proxmox topology provisioning (SDN, LXC, VM)
│   ├── smoke_test.go          # Basic connectivity (DHCP, DNS, NAT, forwarding)
│   ├── multiwan_test.go       # ECMP distribution, WAN failover
│   ├── services_test.go       # FRR, Kea, Unbound, nftables state validation
│   └── lifecycle_test.go      # Image boot, cloud-init, service startup
├── performance/               # iperf3, latency benchmarks (on QCOW2 VMs)
└── support/                   # Shared helpers (SSH client, retry logic, fixtures)

.github/
└── workflows/
    ├── build.yml              # Build LXC + QCOW2 images on push/tag
    ├── test-integration.yml   # Proxmox integration tests (workflow_dispatch)
    └── release.yml            # Publish to GitHub Releases on tag

specs/                         # Existing spec documents (unchanged)
```

---

## Phase 1 — Repository Bootstrap & Minimal Rootfs

**Goal**: Set up the Go module, build tooling, and produce a bootable Debian 13 rootfs with core router packages installed.

| # | Status | Task | Details |
|---|--------|------|---------|
| 1.1 | ✅ | Initialize Go module | `go mod init github.com/fdcastel/warp-router`, set Go 1.22, create `cmd/warp/main.go` stub. Commit: 61bc618 |
| 1.2 | ✅ | Create Makefile with build targets | Targets: `build`, `rootfs`, `lxc`, `qcow2`, `test`, `test-integration`, `clean`. Commit: ca581aa |
| 1.3 | ✅ | Define package list for rootfs | Create `packaging/rootfs/packages.list`: `frr`, `kea-dhcp4-server`, `unbound`, `nftables`, `cloud-init`, `systemd`, `openssh-server`, `iproute2`, `curl`, `ca-certificates`, etc. Commit: fe275b0 |
| 1.4 | ✅ | Write mmdebstrap rootfs build script | `packaging/rootfs/build-rootfs.sh`: produces a clean Debian 13 rootfs directory with all packages, timezone, locale, and SSH config. Must run as root or in a user namespace. Commit: 075bf30 |
| 1.5 | ✅ | Create rootfs overlay files | `packaging/rootfs/overlay/`: default FRR daemons config (enable bgpd, staticd, bfdd, pbrd), sysctl for IP forwarding, nftables base ruleset, Kea skeleton config + Kea systemd override for unprivileged LXC, Unbound skeleton config, SSH hardening. Commit: fb7cc53 |
| 1.6 | ✅ | Write mmdebstrap customize hooks | `packaging/rootfs/hooks/customize01-services.sh`: enable services, set UTC/locale, lock root password, create /etc/warp, clean APT, remove machine-id/SSH host keys. Commit: 2c9a2c1 |
| 1.7 | ✅ | Validate rootfs locally | Structural validation via Go tests (packaging_test.go, 9 tests). Actual mmdebstrap build requires Debian host or privileged VM (seccomp blocks sockets in LXC chroot). Commit: 8a28f8e |

---

## Phase 2 — LXC & QCOW2 Image Builds

**Goal**: Produce publishable LXC template (.tar.zst) and QCOW2 image (.qcow2) from the Phase 1 rootfs.

| # | Status | Task | Details |
|---|--------|------|---------|
| 2.1 | ✅ | Write LXC template build script | `packaging/lxc/build-lxc.sh`: creates Proxmox-compatible `.tar.zst` from rootfs dir with /etc/hostname, /etc/network/interfaces stubs. Commit: 5624ac6 |
| 2.2 | ✅ | Write QCOW2 build script | `packaging/qcow2/build-qcow2.sh`: GPT partitioning (BIOS+EFI+root), ext4, GRUB install, cloud-init NoCloud datasource, qemu-img convert. Commit: c33f490 |
| 2.3 | ✅ | Create cloud-init seed templates | `packaging/cloud-init/seed/`: meta-data, user-data (SSH key injection, warp apply on first boot), network-config (DHCP default). Commit: 9296f7e |
| 2.4 | ✅ | Test LXC image on Proxmox | Built rootfs + LXC template on PVE host (bhs-host51). All 6 services start (frr, nftables, ssh, kea, unbound, networking). Warp binary present. Internet connectivity verified. |
| 2.5 | ⏯️ | Test QCOW2 image on Proxmox manually | Deferred — rootfs built but QCOW2 build requires libguestfs-tools on PVE host. |
| 2.6 | ✅ | Add `make lxc` and `make qcow2` targets | Makefile wired with version from git tags/SHA. QCOW2 target uses sudo. Commit: a066a97 |

---

## Phase 3 — Go CLI & Configuration Model (Phased — Basic)

**Goal**: Implement the `warp` CLI with `validate` and `apply` subcommands that read a YAML site config and drive FRR, nftables, Kea, and Unbound to the desired state.

| # | Status | Task | Details |
|---|--------|------|---------|
| 3.1 | ✅ | Define YAML site config schema | `internal/config/`: Go structs with yaml.v3. Interfaces, DHCP, DNS, Firewall, ECMP, PBR, Sysctl. Parse()/LoadFile(). 5 tests. Commit: 9a49a93 |
| 3.2 | ✅ | Implement config validation | `internal/config/validate.go`: overlapping subnets, conflicting roles, invalid CIDR, missing fields, DHCP/DNS/firewall/PBR cross-references. 19 tests. Commit: 0cab71f |
| 3.3 | ✅ | Implement FRR config renderer | `internal/services/frr/`: static routes, ECMP nexthops, PBR route-maps, BFD peers. Golden file tests. 4 tests. Commit: e7ee1ae |
| 3.4 | ✅ | Implement nftables config renderer | `internal/services/nftables/`: zone-based forwarding, input rules, per-WAN masquerade, default-drop. 4 tests. Commit: 2f535f5 |
| 3.5 | ✅ | Implement Kea DHCP config renderer | `internal/services/kea/`: JSON output with subnets, pools, options, per-interface listening. 4 tests. Commit: 59819e7 |
| 3.6 | ✅ | Implement Unbound config renderer | `internal/services/unbound/`: listen on LAN IPs, access-control, forwarding upstream, DNSSEC hardening. 5 tests. Commit: 4b6e3f4 |
| 3.7 | ✅ | Implement sysctl renderer | `internal/services/sysctl/`: IP forwarding, rp_filter, conntrack_max, security hardening. 3 tests. Commit: 370c1a1 |
| 3.8 | ✅ | Implement apply pipeline | `internal/apply/`: render → atomic write → reload. Stops on first failure. flock guard. 6 tests. Commit: f425366 |
| 3.9 | ✅ | Implement revision store | `internal/revision/`: file-based store with metadata, SHA256, current symlink. 8 tests. Commit: e55b80a |
| 3.10 | ✅ | Implement rollback logic | Previous() + rollback command in CLI. Rollback creates new revision. Commit: e55b80a |
| 3.11 | ✅ | Build `warp` CLI commands | `cmd/warp/`: validate, apply, rollback, revisions, status. Plain subcommands (no cobra). 7 CLI integration tests. Commit: dbe7a03 |
| 3.12 | ✅ | Install `warp` binary into rootfs | build-rootfs.sh installs build/warp to `/usr/bin/warp` and keeps `/usr/local/bin/warp` as compatibility symlink. Also added ifupdown + dns-root-data packages, fixed cloud-init service enablement. |
| 3.13 | ✅ | Add VLAN subinterface provisioning in apply pipeline | `internal/apply/`: `ProvisionVLANs()` creates VLAN devices (`ip link add ... type vlan`), sets interface UP, assigns static IPs, idempotent on repeated apply. Enables Phase 6.12. |
| 3.14 | ✅ | Make revision IDs collision-safe | `internal/revision/store.go`: when multiple `Save()` calls happen within one second, append numeric suffix (`-01`, `-02`, ...). Fixes flaky rollback history tests under fast apply/rollback cycles. |

---

## Phase 4 — WAN Health & Multi-WAN

**Goal**: Implement WAN health monitoring and ECMP/PBR failover so the router can use multiple uplinks safely.

| # | Status | Task | Details |
|---|--------|------|---------|
| 4.1 | ✅ | Implement ICMP health probe | `internal/health/`: goroutine per WAN, injectable PingFunc, state machine (unknown→degraded→down→healthy). OnStateChange callback. 8 tests. |
| 4.2 | ✅ | Implement ECMP route management | `internal/failover/`: RouteManager interface, netlink backend, Controller reacts to health changes. 8 tests with fake route manager. |
| 4.3 | ✅ | Implement PBR rule management | PBR rules in failover controller: removed on uplink down, restored on recovery. Tested in controller tests. |
| 4.4 | ✅ | Implement health status CLI | JSON status file at /run/warp/health.json. `warp status` reads and displays WAN health table. |
| 4.5 | ✅ | Unit tests for health + failover | 8 probe tests + 8 failover tests covering all state transitions. |
| 4.6 | ✅ | Implement monitor daemon command | `cmd/warp/main.go`: added `warp monitor [config]` command. Starts WAN probes, writes `/run/warp/health.json`, handles SIGINT/SIGTERM lifecycle. |
| 4.7 | ✅ | Implement FRR vtysh route manager | `internal/failover/vtysh.go`: route updates through FRR (`vtysh`) instead of netlink to avoid zebra route ownership conflicts. Used by monitor for probe-based failover (Phase 6.6). |

---

## Phase 5 — Test Infrastructure (Proxmox Integration)

**Goal**: Build a Go test suite that provisions test topologies on Proxmox, deploys images, runs tests over SSH, and tears down.

| # | Status | Task | Details |
|---|--------|------|---------|
| 5.1 | ✅ | Set up Go test module under `test/` | `test/go.mod` with x/crypto/ssh. Separate module. |
| 5.2 | ✅ | Implement Proxmox environment config | `testenv/config.go`: SSH-based config (PVE_HOST, PVE_STORAGE, etc.). 3 tests. |
| 5.3 | ✅ | Implement network isolation | `testenv/pve.go`: CreateBridge/DestroyBridge for ephemeral per-run bridges. Replaces SDN zones. |
| 5.4 | ✅ | Implement LXC provisioning helper | `testenv/pve.go`: CreateCT, StartCT, StopCT, DestroyCT, ExecCT, WaitForCT via SSH+pct. |
| 5.5 | ⏯️ | Implement VM provisioning helper | Deferred until QCOW2 images are built (Phase 2). |
| 5.6 | ✅ | Implement SSH test executor | `support/ssh.go` + `testenv/pve.go`: SSH-based command execution on PVE host and via pct exec. |
| 5.7 | ✅ | Implement topology builder | `testenv/topology.go`: NewTopology creates bridge + router/client CTs, t.Cleanup teardown. |
| 5.8 | ✅ | Implement teardown/cleanup | Topology.Teardown destroys CTs (reverse order) + bridge + temp files. |
| 5.9 | ✅ | Write topology smoke test | `integration/smoke_test.go`: 4 subtests (router running, client running, bidirectional ping). Tested on PVE 9.1.4. |
| 5.10 | ✅ | Harden CT teardown for busy ZFS datasets | `testenv/pve.go`: `DestroyCT()` now handles `dataset is busy` by forced ZFS cleanup and verifies CT removal before returning. Stabilizes fixed-VMID integration tests. |

---

## Phase 6 — Integration Tests (Functional)

**Goal**: Test the built images end-to-end on Proxmox. Each test provisions a topology, runs assertions, and tears down.

| # | Status | Task | Details |
|---|--------|------|---------|
| 6.1 | ✅ | LXC image lifecycle test | `lifecycle_test.go`: boots warp-router LXC with public IP. Verifies: 6 services active, warp binary present, /etc/warp exists, internet connectivity. 7 subtests. |
| 6.2 | ⏯️ | QCOW2 image lifecycle test | Deferred — requires QCOW2 image build/runtime environment (Phase 2.5 blocker). |
| 6.3 | ✅ | Basic connectivity test (US1) | `connectivity_test.go`: provisions router+client on internal bridge, applies warp config (validate+apply), verifies IP forwarding, bidirectional ping, warp status. 5 subtests. |
| 6.4 | ✅ | ECMP distribution test (US2) | `ecmp_test.go`: dual-WAN with dummy interfaces, FRR ECMP routes in RIB, dual masquerade, warp status. Also fixed FRR ECMP syntax bug (separate `ip route` commands). 5 subtests. |
| 6.5 | ✅ | WAN failover test (US2) | `failover_test.go`: dual-WAN ECMP with dummy interfaces. Link down → FRR marks nexthop inactive (~1s), only WAN2 in FIB. Link restore → both nexthops recover. Timing subtest: failover in ~800ms (req: ≤3s). 5 subtests. |
| 6.6 | ✅ | WAN probe failover test (US2) | `probe_failover_test.go`: starts `warp monitor`, simulates gateway probe failure with link still UP, verifies monitor removes failed WAN route from FRR via vtysh and restores it on recovery. 6 subtests. |
| 6.7 | ✅ | PBR steering test (US2) | `pbr_test.go`: dual-WAN ECMP + PBR rule (10.99.0.0/24 → wan1). FRR config has pbr-map, policy attached to LAN. vtysh confirms PBR map installed (tableid 10000). WAN1 failure → fallback to WAN2 ECMP. 6 subtests. |
| 6.8 | ✅ | Service health test | `services_test.go/TestServiceHealth`: verifies warp status, all 5 services active after apply, FRR/nftables configs rendered, IP forwarding enabled. 5 subtests. |
| 6.9 | ✅ | Config rollback test | `services_test.go/TestConfigRollback`: apply v1 → apply v2 → rollback → verify v1 restored. Checks FRR hostname, revisions list (3 entries). 4 subtests. |
| 6.10 | ✅ | nftables firewall test | `firewall_test.go`: ruleset loaded, input/forward default drop, LAN→WAN forward allowed, SSH allow, masquerade on WAN, established/related, drop counters, client ping. 9 subtests. |
| 6.11 | ✅ | DHCP service test | `dhcp_test.go`: Kea active, pool config rendered, client DHCP lease acquisition via dhclient, lease file verification. 4 subtests. |
| 6.12 | ✅ | VLAN subinterface test | `vlan_test.go`: verifies VLAN subinterface creation (`dummy0.100`, VID 100), interface up state, IP assignment, DHCP/nftables integration, and apply idempotency. 7 subtests. |

---

## Phase 7 — Performance Tests

**Goal**: Validate throughput and latency on QCOW2 VM images. These tests run on Proxmox VMs (not LXC) for accurate kernel-level measurements.

| # | Status | Task | Details |
|---|--------|------|---------|
| 7.1 | ❌ | Install iperf3 and measurement tools in test images | Add `iperf3`, `hping3`, `fping` to WAN gateway and LAN client images. |
| 7.2 | ❌ | Write throughput benchmark test | LAN client → iperf3 server on WAN gateway, through router VM. Measure bidirectional throughput. Target: ≥500 Mbps IMIX (no XDP in v1). Maps to SC-006 (adjusted for no-XDP). |
| 7.3 | ❌ | Write latency benchmark test | LAN client → hping3 to WAN gateway through router. Measure p99 forwarding latency. Target: <1 ms p99 at target throughput. Maps to SC-007. |
| 7.4 | ❌ | Write failover timing test | Measure actual time from WAN link down to new flows using backup. Target: ≤3 s. Automated measurement with timestamps. |
| 7.5 | ❌ | Write scale configuration test | Config with 4 WANs, 16 LAN/VLAN segments, 500 PBR rules. Apply and verify all services start, ECMP works, PBR routes installed. Maps to SC-008. |

---

## Phase 8 — CI/CD & Release Pipeline

**Goal**: Automate image builds and release publication via GitHub Actions.

| # | Status | Task | Details |
|---|--------|------|---------|
| 8.1 | ✅ | Create `build.yml` workflow | Added `.github/workflows/build.yml` with push/PR/tag triggers, unit tests, image builds, artifact upload, and rootfs + Go cache usage. |
| 8.2 | ❌ | Create `test-integration.yml` workflow | Trigger: `workflow_dispatch` with inputs for image artifact URL and Proxmox target. Steps: download image artifacts, run `go test ./test/integration/...` with Proxmox env vars from GitHub secrets. Produce JUnit XML report. |
| 8.3 | ✅ | Create `release.yml` workflow | Added `.github/workflows/release.yml` on tag push (`v*`) to build artifacts, generate checksums, and publish GitHub Release assets. |
| 8.4 | ❌ | Configure GitHub secrets | Document required secrets: `PROXMOX_API_URL`, `PROXMOX_API_TOKEN_ID`, `PROXMOX_API_TOKEN_SECRET`, `PROXMOX_NODE`, `PROXMOX_SSH_KEY`. Add to repo settings. |
| 8.5 | ✅ | Add artifact naming and versioning | Makefile `VERSION` now resolves to exact tag on releases or `dev-<short-sha>` on non-tag builds; artifact names include this version. |
| 8.6 | ✅ | Add build caching | Added `actions/cache` for rootfs keyed by rootfs inputs and enabled Go module/build cache via `actions/setup-go`. |

---

## Phase 9 — Hardening & Documentation

**Goal**: Security hardening, operational documentation, and polish.

| # | Status | Task | Details |
|---|--------|------|---------|
| 9.1 | ✅ | Security hardening of base image | SSH password auth disabled in overlay, secure sysctl baseline already present, removed unnecessary package (`nano`), added `unattended-upgrades` package and apt unattended security policy in overlay (`20auto-upgrades`, `52warp-security-upgrades`). |
| 9.2 | ✅ | Write operator quickstart guide | Added `doc/quickstart.md` with build, deploy, validate/apply, connectivity verification, and rollback flow. |
| 9.3 | ✅ | Write site config reference | Added `doc/site-config-reference.md` documenting schema fields, validation rules, defaults, and full example config. |
| 9.4 | ✅ | Write test infrastructure README | Added `test/README.md` covering Proxmox prerequisites, environment variables, execution commands, and troubleshooting flow. |
| 9.5 | ✅ | Add README.md to repo root | Added root `README.md` with project overview, quick start, docs links, workflow badge, release link, and Proxmox-Automation references. |
| 9.6 | ❌ | Validate full release cycle end-to-end | Tag a release, verify CI builds images, publishes to GitHub Releases, integration tests pass against published images. |

---

## Dependencies Between Phases

```
Phase 1 (Rootfs)
    │
    ▼
Phase 2 (LXC + QCOW2 images)
    │
    ├──────────────────────┐
    ▼                      ▼
Phase 3 (CLI/Config)    Phase 5 (Test infra)
    │                      │
    ▼                      ▼
Phase 4 (Multi-WAN)    Phase 6 (Integration tests) ◄── requires Phase 3
    │                      │
    │                      ▼
    │                   Phase 7 (Performance tests) ◄── requires Phase 4
    │                      │
    ▼                      ▼
Phase 8 (CI/CD) ◄──── requires Phase 2 + Phase 6
    │
    ▼
Phase 9 (Hardening/Docs)
```

**Parallelization opportunities:**
- Phase 3 (CLI) and Phase 5 (test infra) can proceed in parallel after Phase 2.
- Phase 8 (CI/CD) can start early for the build pipeline (8.1) once Phase 2 is done; integration workflow (8.2) needs Phase 5+6.

---

## Environment Variables & Secrets Reference

| Variable | Description | Used By |
|----------|-------------|---------|
| `PVE_HOST` | Proxmox VE SSH host (e.g., `bhs-host51.dw.net.br`) | Test suite |
| `PVE_USER` | SSH user on PVE host (default: `root`) | Test suite |
| `SSH_KEY_PATH` | SSH private key path (default: `~/.ssh/id_ed25519`) | Test suite |
| `PVE_STORAGE` | Storage pool for test disks (default: `spool-zfs`) | Test suite |
| `PVE_TEMPLATE` | LXC template reference (default: `local:vztmpl/debian-12-standard_12.7-1_amd64.tar.zst`) | Test suite |
| `VMID_BASE` | Starting VMID for test containers (default: `9000`) | Test suite |

---

## Out of Scope for v1 (Revisit in v2)

- XDP/eBPF fast-path filtering
- IPv6 support
- PPPoE WAN addressing
- Multi-user RBAC and remote management API
- Multiple tagged trunks / advanced bridge domains
- BGP peering with external routers (FRR capable, but not tested/documented in v1)
- HA / clustering
- Web UI
- OCI registry publication (GitHub Releases only for v1)

---

## Phase 10 — Code Review — 2026-04-14

> Full-codebase audit covering `cmd/`, `internal/`, `test/`, and `packaging/`.
> Outcome: all actionable review items were implemented and validated; the QCOW2-specific item remains deferred because QCOW2 work itself is still deferred.

### Resolution Summary

| Item | Status | Resolution |
|------|--------|------------|
| CR-1 | ✅ RESOLVED | `warp apply` and `warp rollback` now save revisions only after a successful pipeline run. |
| CR-2 | ✅ RESOLVED | Apply pipeline now backs up rendered config targets and restores them on failure. |
| CR-3 | ✅ RESOLVED | Documented probe-based PBR failover as unsupported in v1; `vtysh` PBR methods now state the limitation explicitly. |
| CR-4 | ✅ RESOLVED | `make test-integration` now runs from the `test/` module. |
| CR-5 | ✅ RESOLVED | Health probe state-change callbacks now run after releasing the prober mutex. |
| CR-6 | ✅ RESOLVED | Failover controller now logs ECMP/PBR update failures and reverts in-memory active state on ECMP errors. |
| CR-7 | ✅ RESOLVED | YAML parsing now uses `KnownFields(true)` and rejects unknown keys. |
| CR-8 | ✅ RESOLVED | Removed unsupported schema fields (`mtu`, `weight`, `table`) and aligned renderers/tests with the supported config model. |
| CR-9 | ✅ RESOLVED | Unbound renderer now returns a disabled stub config when `dns.enabled: false`. |
| CR-10 | ✅ RESOLVED | VLAN provisioning now reconciles addresses, creates missing VLANs, and removes stale subinterfaces. |
| CR-11 | ✅ RESOLVED | Revision lookups now validate revision IDs before reading from disk. |
| CR-12 | ✅ RESOLVED | Rootfs nftables overlay no longer allows forwarding before `warp apply` installs policy. |
| CR-13 | ✅ RESOLVED | FRR renderer now merges PBR rules into a single attached map instead of silently dropping later maps. |
| CR-14 | ✅ RESOLVED | DHCP validation now enforces pool bounds within the configured subnet. |
| CR-15 | ⏯️ DEFERRED | QCOW2 GRUB-install hardening remains deferred until QCOW2 build/testing work resumes. |
| CR-16 | ✅ RESOLVED | Added shared integration-test helpers for config apply, dummy WAN setup, template reuse, and polling. |
| CR-17 | ✅ RESOLVED | `services_test.go` now uses the topology allocator instead of fixed VMIDs. |

### Validation

- `go test ./...`
- `cd test && go test -tags integration -run '^$' ./integration/...`

### Notes

- Phase 10 is complete for all non-deferred items.
- CR-15 stays deferred because QCOW2 image work is still blocked by the project-level environment constraint already documented elsewhere.
