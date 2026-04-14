# Warp Router

Warp Router is a Linux router appliance built on Debian 13 with a Go control plane.

It configures and manages:

- FRR for routing, ECMP, and PBR
- nftables for firewalling and NAT
- Kea for DHCPv4
- Unbound for DNS resolver/forwarder

## Status

Current implementation progress is tracked in:

- `doc/WARP_ROUTER_PLAN.md`
- `doc/WARP_ROUTER_DECISIONS.md`

Release artifacts are published through [GitHub Releases](https://github.com/fdcastel/warp-router/releases).

## Documentation

- Operator quickstart: [`doc/quickstart.md`](doc/quickstart.md)
- Site config reference: [`doc/site-config-reference.md`](doc/site-config-reference.md)
- Integration test guide: [`test/README.md`](test/README.md)
- Project roadmap and task status: [`doc/WARP_ROUTER_PLAN.md`](doc/WARP_ROUTER_PLAN.md)
- Architecture decisions and lessons learned: [`doc/WARP_ROUTER_DECISIONS.md`](doc/WARP_ROUTER_DECISIONS.md)

## Example

Reference helper scripts from [Proxmox-Automation](https://github.com/fdcastel/Proxmox-Automation) project.

### Create and configure a working basic router

Run on a Proxmox host as root:

```bash
apt-get update && apt-get install -y jq

source <(curl -Ls https://bit.ly/p-v-a)

CT_ID=9100
CT_NAME=warp-router
WAN_BRIDGE=vmbr0
LAN_BRIDGE=vmbrloc0        # Do NOT use your LAN here! (will start a DHCP server on it).
LAN_CIDR=192.168.50.1/24

ASSET_URL="$(curl -fsSL https://api.github.com/repos/fdcastel/warp-router/releases/latest \
	| jq -r '.assets[] | select(.name | test("\\.tar\\.zst$")) | .browser_download_url' \
	| head -n1)"

if [ -z "$ASSET_URL" ] || [ "$ASSET_URL" = "null" ]; then
	echo "No .tar.zst release asset found for latest warp-router release" >&2
	exit 1
fi

TEMPLATE_FILE="$(basename "$ASSET_URL")"
curl -fL "$ASSET_URL" -o "/var/lib/vz/template/cache/$TEMPLATE_FILE"

./new-ct.sh "$CT_ID" \
	--ostemplate "local:vztmpl/$TEMPLATE_FILE" \
	--hostname "$CT_NAME" \
	--sshkeys /root/.ssh/id_ed25519.pub \
	--rootfs local-zfs:8 \
	--bridge "$WAN_BRIDGE" \
	--memory 2048 \
	--cores 2 \
	--net1 "name=eth1,bridge=$LAN_BRIDGE,ip=$LAN_CIDR"

# Write basic router config and apply it inside the container.
pct exec "$CT_ID" -- bash <<'OUTER_EOF'
cat > /etc/warp/site.yaml <<'EOF'
hostname: edge-basic
interfaces:
  - name: wan1
    role: wan
    device: eth0
    address: dhcp

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
EOF

warp validate /etc/warp/site.yaml
warp apply /etc/warp/site.yaml
warp status
systemctl --no-pager --full status frr nftables kea-dhcp4-server unbound | sed -n '1,40p'
OUTER_EOF

echo "Router CT $CT_ID is up."
echo "Attach a client to bridge $LAN_BRIDGE and test DHCP + internet access."
```

## Development

Core commands:

```bash
make build
make test
make test-integration
```
