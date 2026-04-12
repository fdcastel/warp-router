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
| 1.5 | ✅ | Create rootfs overlay files | `packaging/rootfs/overlay/`: default FRR daemons config (enable bgpd, staticd, bfdd, pbrd), sysctl for IP forwarding, nftables base ruleset, Kea skeleton config, Unbound skeleton config, SSH hardening. Commit: fb7cc53 |
| 1.6 | ✅ | Write mmdebstrap customize hooks | `packaging/rootfs/hooks/customize01-services.sh`: enable services, set UTC/locale, lock root password, create /etc/warp, clean APT, remove machine-id/SSH host keys. Commit: 2c9a2c1 |
| 1.7 | ✅ | Validate rootfs locally | Structural validation via Go tests (packaging_test.go, 9 tests). Actual mmdebstrap build requires Debian host or privileged VM (seccomp blocks sockets in LXC chroot). Commit: 8a28f8e |

---

## Phase 2 — LXC & QCOW2 Image Builds

**Goal**: Produce publishable LXC template (.tar.zst) and QCOW2 image (.qcow2) from the Phase 1 rootfs.

| # | Status | Task | Details |
|---|--------|------|---------|
| 2.1 | ❌ | Write LXC template build script | `packaging/lxc/build-lxc.sh`: takes rootfs dir, creates Proxmox-compatible `.tar.zst` archive with correct metadata (`/etc/hostname`, `/etc/network/interfaces` stubs). Output: `warp-router-<version>-lxc-amd64.tar.zst` |
| 2.2 | ❌ | Write QCOW2 build script | `packaging/qcow2/build-qcow2.sh`: create raw disk image, partition (EFI + root), format ext4, copy rootfs, install GRUB, inject cloud-init datasource config (NoCloud), convert to qcow2. Use `virt-customize` or `guestfish` for post-processing. Output: `warp-router-<version>-amd64.qcow2` |
| 2.3 | ❌ | Create cloud-init seed templates | `packaging/cloud-init/seed/`: `meta-data`, `user-data`, `network-config` templates. Default user-data enables SSH key injection, hostname setting, and runs `warp apply` on first boot (placeholder for Phase 3). |
| 2.4 | ❌ | Test LXC image on Proxmox manually | Upload `.tar.zst` to Proxmox, create LXC container (`pct create`), start it, verify FRR/Kea/Unbound/nftables start, SSH accessible |
| 2.5 | ❌ | Test QCOW2 image on Proxmox manually | Import qcow2 (`qm importdisk`), attach cloud-init drive, boot VM, verify cloud-init runs, SSH accessible, FRR/Kea/Unbound/nftables start |
| 2.6 | ❌ | Add `make lxc` and `make qcow2` targets | Wire up the Makefile to call build scripts with proper versioning (git tag or `dev-<sha>`) |

---

## Phase 3 — Go CLI & Configuration Model (Phased — Basic)

**Goal**: Implement the `warp` CLI with `validate` and `apply` subcommands that read a YAML site config and drive FRR, nftables, Kea, and Unbound to the desired state.

| # | Status | Task | Details |
|---|--------|------|---------|
| 3.1 | ❌ | Define YAML site config schema | `internal/config/`: Go structs for site config — interfaces (WAN/LAN roles, addressing mode), DHCP scopes, DNS settings, firewall zones, ECMP policy, PBR rules. Use `gopkg.in/yaml.v3`. Write unit tests for parsing and validation. |
| 3.2 | ❌ | Implement config validation | `internal/config/validate.go`: reject overlapping subnets, conflicting roles, incomplete service intent, invalid CIDR, missing required fields. Table-driven tests. |
| 3.3 | ❌ | Implement FRR config renderer | `internal/services/frr/`: render `frr.conf` from site config — static routes, ECMP nexthops, PBR route-maps, BFD peers (optional). Unit tests with golden files. |
| 3.4 | ❌ | Implement nftables config renderer | `internal/services/nftables/`: render nftables ruleset — zones, forwarding policy, per-WAN masquerade, conntrack, default drop. Unit tests with golden files. |
| 3.5 | ❌ | Implement Kea DHCP config renderer | `internal/services/kea/`: render `kea-dhcp4.conf` JSON — interfaces, subnets, pools, lease time, DNS server option (point to Unbound). Unit tests. |
| 3.6 | ❌ | Implement Unbound config renderer | `internal/services/unbound/`: render `unbound.conf` — listen interfaces, access-control for LAN subnets, forwarding upstream. Unit tests. |
| 3.7 | ❌ | Implement sysctl/ip-forward renderer | Set `net.ipv4.ip_forward=1`, per-interface `rp_filter`, `nf_conntrack_max` tuning. |
| 3.8 | ❌ | Implement apply pipeline | `internal/apply/`: orchestrate config renders, write files atomically, reload services (`systemctl reload frr`, `nft -f`, `systemctl restart kea-dhcp4-server`, `systemctl restart unbound`), verify service health post-apply. Single-writer lock via flock. |
| 3.9 | ❌ | Implement revision store | `internal/revisions/`: keep current + last-known-good config on disk. On apply: backup current → last-known-good, write new → current. On failure: restore last-known-good. |
| 3.10 | ❌ | Implement rollback logic | On partial apply failure, restore all subsystem configs from last-known-good revision and reload. Report which step failed. |
| 3.11 | ❌ | Build `warp` CLI commands | `cmd/warp/`: `warp validate <config>`, `warp apply <config>`, `warp status`, `warp rollback`. Use `cobra` or plain `flag`+subcommands. |
| 3.12 | ❌ | Install `warp` binary into rootfs | Update mmdebstrap hooks to copy the built `warp` binary to `/usr/local/bin/warp` in the image. Update cloud-init user-data to call `warp apply` on first boot. |

---

## Phase 4 — WAN Health & Multi-WAN

**Goal**: Implement WAN health monitoring and ECMP/PBR failover so the router can use multiple uplinks safely.

| # | Status | Task | Details |
|---|--------|------|---------|
| 4.1 | ❌ | Implement ICMP health probe | `internal/health/`: goroutine per WAN uplink, 1 Hz ICMP echo to configured target (default: gateway). 3 consecutive failures = uplink down. Carrier loss = immediate down. Expose health via shared state. |
| 4.2 | ❌ | Implement ECMP route management | When health state changes: update kernel ECMP routes via `vishvananda/netlink` — add/remove nexthops. Keep per-uplink masquerade rules consistent. |
| 4.3 | ❌ | Implement PBR rule management | `internal/services/frr/pbr.go` or kernel ip-rule management: install policy routing rules from config. On targeted uplink failure, remove PBR rule so traffic falls back to ECMP. Restore when uplink recovers. |
| 4.4 | ❌ | Implement health status CLI | `warp status` shows per-uplink health, probe results, active ECMP nexthops, PBR rules, conntrack counts. |
| 4.5 | ❌ | Unit tests for health + failover | Test state machine transitions: healthy → failed → recovered. Mock netlink calls. |

---

## Phase 5 — Test Infrastructure (Proxmox Integration)

**Goal**: Build a Go test suite that provisions test topologies on Proxmox, deploys images, runs tests over SSH, and tears down.

| # | Status | Task | Details |
|---|--------|------|---------|
| 5.1 | ❌ | Set up Go test module under `test/` | `test/go.mod` (or use root module with build tags). Import `luthermonson/go-proxmox`, `golang.org/x/crypto/ssh`. |
| 5.2 | ❌ | Implement Proxmox environment config | `test/integration/testenv/config.go`: read Proxmox API URL, token/credentials, node name, storage pool, and test network config from env vars or a `.env` file. Never hardcode secrets. |
| 5.3 | ❌ | Implement SDN zone provisioning | `test/integration/testenv/sdn.go`: create isolated SDN zone + vnets (WAN-sim-1, WAN-sim-2, LAN-sim) per test run, with unique names derived from test run ID. Cleanup on teardown. |
| 5.4 | ❌ | Implement LXC provisioning helper | `test/integration/testenv/lxc.go`: upload LXC template, create container with specified NICs attached to test vnets, start container, wait for SSH. Return SSH connection. |
| 5.5 | ❌ | Implement VM provisioning helper | `test/integration/testenv/vm.go`: upload QCOW2, create VM, attach cloud-init drive with test user-data, set NICs to test vnets, start VM, wait for SSH. Return SSH connection. |
| 5.6 | ❌ | Implement SSH test executor | `test/support/ssh.go`: connect via SSH, run commands, capture stdout/stderr, assert exit codes. Support SCP for file transfer. Retry logic for boot-time SSH availability. |
| 5.7 | ❌ | Implement topology builder | `test/integration/testenv/topology.go`: compose a full test topology: router (LXC or VM) + WAN gateway(s) (lightweight LXC) + LAN client (lightweight LXC). Configure gateway LXCs with IP forwarding + masquerade to simulate ISPs. |
| 5.8 | ❌ | Implement teardown/cleanup | `test/integration/testenv/cleanup.go`: stop and destroy all containers/VMs, remove SDN zones/vnets, remove uploaded templates. Run even on test failure (use `t.Cleanup()`). |
| 5.9 | ❌ | Write topology smoke test | Verify: topology provisions successfully, all nodes reachable via SSH, router can ping WAN gateways, LAN client can ping router. This validates the test infrastructure itself. |

---

## Phase 6 — Integration Tests (Functional)

**Goal**: Test the built images end-to-end on Proxmox. Each test provisions a topology, runs assertions, and tears down.

| # | Status | Task | Details |
|---|--------|------|---------|
| 6.1 | ❌ | LXC image lifecycle test | Boot LXC image, verify: all services start (FRR, Kea, Unbound, nftables), SSH accessible, `warp` binary present, `/etc/warp/site.yaml` exists or can be placed. |
| 6.2 | ❌ | QCOW2 image lifecycle test | Boot QCOW2 with cloud-init, verify: cloud-init completes, hostname set, SSH key injected, all services start, `warp` binary present. |
| 6.3 | ❌ | Basic connectivity test (US1) | Apply a single-WAN + single-LAN config. From LAN client: get DHCP lease, resolve DNS name, ping internet (via WAN gateway), verify NAT (masquerade). Maps to SC-001. |
| 6.4 | ❌ | ECMP distribution test (US2) | Apply dual-WAN config with equal ECMP weights. Generate multiple flows from LAN client. Verify traffic distributes across both WAN gateways (check conntrack or gateway packet counts). Maps to SC-002. |
| 6.5 | ❌ | WAN failover test (US2) | With dual-WAN ECMP active: `ip link set wan1 down` on router (carrier loss). Verify new flows shift to WAN2 within 3 seconds. Restore WAN1, verify recovery. Maps to SC-003. |
| 6.6 | ❌ | WAN probe failover test (US2) | With dual-WAN ECMP: block ICMP on WAN gateway 1 (firewall). Verify router detects failure via probe timeout (~3s), shifts traffic. Maps to SC-003 (probe path). |
| 6.7 | ❌ | PBR steering test (US2) | Apply config with PBR rule steering specific source subnet to WAN1. Verify matching traffic uses WAN1. Fail WAN1, verify fallback to ECMP on WAN2. Maps to FR-007. |
| 6.8 | ❌ | Service health test (US3) | Verify `warp status` output includes uplink health, service states, active revision. Maps to SC-004. |
| 6.9 | ❌ | Config rollback test (US3) | Apply a valid config, then apply a broken config. Verify system rolls back to previous working state within 30 seconds. Verify services remain functional. Maps to SC-005. |
| 6.10 | ❌ | nftables firewall test | Verify: default deny inbound on WAN, allow established/related, LAN-to-WAN forwarding permitted, WAN-to-LAN blocked unless explicitly allowed. |
| 6.11 | ❌ | DHCP edge case tests | Verify: full pool exhaustion behavior, lease renewal, multiple LAN segments get distinct pools. |
| 6.12 | ❌ | VLAN subinterface test | Apply config with 802.1Q VLAN LAN segments on a tagged trunk. Verify DHCP/DNS/NAT work per VLAN segment. |

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
| 8.1 | ❌ | Create `build.yml` workflow | Trigger: push to main, pull request, tag. Steps: checkout, install build deps (mmdebstrap, libguestfs-tools, qemu-utils), build rootfs, build LXC, build QCOW2. Upload artifacts. Run `go test ./...` (unit tests). |
| 8.2 | ❌ | Create `test-integration.yml` workflow | Trigger: `workflow_dispatch` with inputs for image artifact URL and Proxmox target. Steps: download image artifacts, run `go test ./test/integration/...` with Proxmox env vars from GitHub secrets. Produce JUnit XML report. |
| 8.3 | ❌ | Create `release.yml` workflow | Trigger: tag push (`v*`). Steps: build LXC and QCOW2 images, compute SHA256 checksums, create GitHub Release, upload `.tar.zst`, `.qcow2`, and `checksums.txt` as release assets. |
| 8.4 | ❌ | Configure GitHub secrets | Document required secrets: `PROXMOX_API_URL`, `PROXMOX_API_TOKEN_ID`, `PROXMOX_API_TOKEN_SECRET`, `PROXMOX_NODE`, `PROXMOX_SSH_KEY`. Add to repo settings. |
| 8.5 | ❌ | Add artifact naming and versioning | All artifacts include version derived from git tag (release) or `dev-<short-sha>` (CI). Example: `warp-router-v0.1.0-lxc-amd64.tar.zst`, `warp-router-v0.1.0-amd64.qcow2`. |
| 8.6 | ❌ | Add build caching | Cache mmdebstrap rootfs (keyed on packages.list hash), Go module cache, and Go build cache in CI to speed up builds. |

---

## Phase 9 — Hardening & Documentation

**Goal**: Security hardening, operational documentation, and polish.

| # | Status | Task | Details |
|---|--------|------|---------|
| 9.1 | ❌ | Security hardening of base image | Disable password auth on SSH, remove unnecessary packages, set secure sysctl defaults (rp_filter, syncookies, ICMP redirect disabled), configure unattended-upgrades for security patches. |
| 9.2 | ❌ | Write operator quickstart guide | `docs/quickstart.md`: download image, deploy on Proxmox (LXC or VM), write site config, validate, apply, verify connectivity. |
| 9.3 | ❌ | Write site config reference | `docs/site-config-reference.md`: document every YAML field with examples, defaults, and validation rules. |
| 9.4 | ❌ | Write test infrastructure README | `test/README.md`: how to set up a Proxmox test environment, required env vars, run integration tests locally, interpret results. |
| 9.5 | ❌ | Add README.md to repo root | Project overview, quick install, link to docs, build status badge, release link. |
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
| `PROXMOX_API_URL` | Proxmox VE API endpoint (e.g., `https://pve.example.com:8006/api2/json`) | Test suite, CI |
| `PROXMOX_API_TOKEN_ID` | API token ID (e.g., `user@pam!tokenname`) | Test suite, CI |
| `PROXMOX_API_TOKEN_SECRET` | API token secret | Test suite, CI |
| `PROXMOX_NODE` | Proxmox node name to provision on | Test suite, CI |
| `PROXMOX_STORAGE` | Storage pool for images/disks (default: `local`) | Test suite, CI |
| `PROXMOX_SSH_HOST` | SSH host for direct node access (if needed) | Test suite |
| `PROXMOX_SSH_KEY` | SSH private key for node access | Test suite, CI |
| `WARP_TEST_IMAGE_LXC` | Path or URL to LXC template under test | Test suite |
| `WARP_TEST_IMAGE_QCOW2` | Path or URL to QCOW2 image under test | Test suite |

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
