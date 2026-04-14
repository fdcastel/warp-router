# Warp Router Operator Quickstart

This guide gets a Warp Router LXC running on Proxmox and applies a working site config.

## Prerequisites

- Proxmox VE host reachable by SSH.
- A built Warp Router LXC template (`warp-router-<version>.tar.zst`).
- One public/WAN bridge on Proxmox (example: `vmbr0`).
- One LAN bridge on Proxmox for client networks.

## 1) Build the binary and LXC template

From repository root:

```bash
make build
sudo make rootfs
sudo make lxc
```

Artifact output is written under `build/`.

## 2) Upload and deploy on Proxmox

Copy the template to your PVE host storage and create a container.

If you use the helper scripts from https://github.com/fdcastel/Proxmox-Automation, follow that repository workflow to provision guests quickly.

Manual example on the PVE host:

```bash
# Example values, adjust for your environment.
pct create 9100 local:vztmpl/warp-router-dev.tar.zst \
  --hostname warp-router \
  --cores 2 --memory 2048 --swap 512 \
  --rootfs spool-zfs:8 \
  --net0 name=eth0,bridge=vmbr0,ip=dhcp,type=veth \
  --net1 name=eth1,bridge=vmbr10,ip=192.168.50.1/24,type=veth \
  --unprivileged 1 --features nesting=1

pct start 9100
```

## 3) Write the site config

Create `/etc/warp/site.yaml` inside the router container:

```yaml
hostname: edge-1
interfaces:
  - name: wan1
    role: wan
    device: eth0
    address: dhcp
    gateway: 100.64.0.1

  - name: lan1
    role: lan
    device: eth1
    address: 192.168.50.1/24

dhcp:
  enabled: true
  subnets:
    - subnet: 192.168.50.0/24
      interface: lan1
      pool_start: 192.168.50.100
      pool_end: 192.168.50.199
      gateway: 192.168.50.1
      dns_servers:
        - 192.168.50.1

dns:
  enabled: true
  forwarders:
    - 1.1.1.1
    - 8.8.8.8

ecmp:
  enabled: false
```

## 4) Validate and apply

Run inside the router:

```bash
warp validate /etc/warp/site.yaml
warp apply /etc/warp/site.yaml
warp status
```

Expected result:

- Validation passes with no errors.
- `warp apply` completes all service render/reload steps.
- `warp status` shows a current revision.

## 5) Verify connectivity

On the router:

```bash
systemctl is-active frr nftables kea-dhcp4-server unbound ssh
nft list ruleset
ip route show
```

From a LAN client attached to the LAN bridge:

- Obtain DHCP lease.
- Ping the router LAN IP.
- Resolve DNS through router LAN IP.
- Reach internet through NAT.

## 6) Rollback (if needed)

```bash
warp revisions
warp rollback
```

Rollback reapplies the previous revision and stores rollback as a new revision entry.
