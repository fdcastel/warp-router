# Integration Test Infrastructure

This directory contains Proxmox-backed integration tests for Warp Router images.

## Overview

- Tests run from Go in `test/integration`.
- Provisioning is performed over SSH to the Proxmox host using `pct` commands.
- Tests create isolated bridges and containers, run assertions, then tear down resources.

## Prerequisites

- Access to a Proxmox VE host by SSH.
- A Warp Router LXC template available in Proxmox storage.
- A private key that can SSH to the Proxmox host as a user with `pct` privileges.
- Go toolchain matching `test/go.mod`.

## Environment Variables

The harness reads these variables (see `test/integration/testenv/config.go`):

- `PVE_HOST`: Proxmox host (default: `bhs-host51.dw.net.br`)
- `PVE_USER`: SSH user (default: `root`)
- `PVE_SSH_KEY`: SSH private key path (default: `$HOME/.ssh/id_ed25519`)
- `PVE_STORAGE`: Proxmox storage name for container disks (default: `spool-zfs`)
- `PVE_TEMPLATE`: LXC template (default in code is Debian template; set this to Warp Router template)
- `PVE_WAN_BRIDGE`: Existing WAN bridge on host (default: `vmbr0`)
- `PVE_VMID_BASE`: Base VMID for test allocations (default: `9000`)

Recommended export block:

```bash
export PVE_HOST=bhs-host51.dw.net.br
export PVE_USER=root
export PVE_SSH_KEY=$HOME/.ssh/id_ed25519
export PVE_STORAGE=spool-zfs
export PVE_TEMPLATE=local:vztmpl/warp-router-dev.tar.zst
export PVE_WAN_BRIDGE=vmbr0
export PVE_VMID_BASE=9000
```

## Running Tests

From repository root:

```bash
make test-integration
```

Or run directly:

```bash
cd test
go test ./integration/... -v -timeout 60m
```

Run a single test file:

```bash
cd test
go test ./integration -run TestConnectivity -v -timeout 30m
```

## Interpreting Results

Typical validation points per test:

- Router services are active (`frr`, `nftables`, `kea-dhcp4-server`, `unbound`, `ssh`).
- `warp validate` and `warp apply` succeed.
- Routing, NAT, DHCP, DNS, failover, and rollback behave as expected.

When failures occur:

- Check test output for the failed shell command and stderr.
- Inspect resources on Proxmox (`pct list`, `pct config <vmid>`, `ip -br link`).
- Re-run with `-run` to isolate a single scenario.

## Safety Notes

- Do not modify host-global networking configuration.
- Tests create and remove only ephemeral test resources.
- If teardown fails, remove stale resources manually before rerunning.
