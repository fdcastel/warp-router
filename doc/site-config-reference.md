# Warp Router Site Config Reference

This document describes the YAML schema consumed by `warp validate` and `warp apply`.

## Top-Level Schema

```yaml
hostname: string
interfaces: []
dhcp: {}          # optional
dns: {}           # optional
firewall: {}      # optional
ecmp: {}          # optional
pbr: []           # optional
sysctl: {}        # optional
```

## hostname

- Type: string
- Required: yes
- Validation: must be non-empty

## interfaces

- Type: array of interface objects
- Required: yes
- Validation:
  - At least one interface is required.
  - At least one `wan` and one `lan` role must exist.
  - `name` must be unique.
  - `device` must be unique.
  - Static `address` must be valid CIDR.
  - Subnets across interfaces must not overlap.

### interfaces[].name

- Type: string
- Required: yes
- Example: `wan1`, `lan1`

### interfaces[].role

- Type: string
- Required: yes
- Allowed values: `wan`, `lan`

### interfaces[].device

- Type: string
- Required: yes
- Linux device name.
- Example: `eth0`, `eth1`, `eth2.100`

### interfaces[].address

- Type: string
- Required: yes
- Allowed values:
  - `dhcp`
  - CIDR value (example: `192.168.10.1/24`)

### interfaces[].gateway

- Type: string
- Required: no
- Validation: if present, must be valid IP address.
- Typical use: WAN interfaces.

### interfaces[].vlan

- Type: integer
- Required: no
- Default: `0` (untagged)
- Validation:
  - Range: `0..4094`
  - When `> 0`, `device` must be dotted form (example: `eth1.100`).

### interfaces[].health_check

Optional WAN probe settings used by `warp monitor`.

#### interfaces[].health_check.target

- Type: string
- Required: no
- Default: interface gateway

#### interfaces[].health_check.interval

- Type: integer seconds
- Required: no
- Default: 1

#### interfaces[].health_check.timeout

- Type: integer seconds
- Required: no
- Default: 2

#### interfaces[].health_check.failures

- Type: integer
- Required: no
- Default: 3

## dhcp

### dhcp.enabled

- Type: boolean
- Required: yes when `dhcp` section is present

### dhcp.subnets

- Type: array
- Required: yes when `dhcp.enabled=true`

#### dhcp.subnets[].subnet

- Type: CIDR string
- Required: yes

#### dhcp.subnets[].interface

- Type: string
- Required: yes
- Validation: must reference an existing interface name.

#### dhcp.subnets[].pool_start

- Type: IP string
- Required: yes
- Validation: must be inside `subnet`.

#### dhcp.subnets[].pool_end

- Type: IP string
- Required: yes
- Validation: must be inside `subnet`.

#### dhcp.subnets[].gateway

- Type: IP string
- Required: yes

#### dhcp.subnets[].dns_servers

- Type: array of IP strings
- Required: no

#### dhcp.subnets[].lease_time

- Type: integer seconds
- Required: no
- Default in renderer: 3600

#### dhcp.subnets[].options

- Type: map of string to string
- Required: no

## dns

### dns.enabled

- Type: boolean
- Required: yes when `dns` section is present

### dns.listen

- Type: array of IP strings
- Required: no
- Default: LAN IPs plus loopback (`127.0.0.1`).

### dns.forwarders

- Type: array of IP strings
- Required: no
- Behavior: empty means full recursion mode.

### dns.allow_from

- Type: array of CIDR strings
- Required: no
- Default: LAN subnets.

## firewall

### firewall.zones

- Type: array
- Required: no

#### firewall.zones[].name

- Type: string
- Required: yes

#### firewall.zones[].interfaces

- Type: array of interface names
- Required: yes
- Validation: each interface must exist.

### firewall.forward_rules

- Type: array
- Required: no

#### firewall.forward_rules[].from

- Type: string
- Required: yes
- Validation: must reference existing zone.

#### firewall.forward_rules[].to

- Type: string
- Required: yes
- Validation: must reference existing zone.

#### firewall.forward_rules[].action

- Type: string
- Required: yes
- Allowed: `accept`, `drop`

#### firewall.forward_rules[].protocol

- Type: string
- Required: no
- Typical values: `tcp`, `udp`, `icmp`

#### firewall.forward_rules[].port

- Type: string
- Required: no
- Example: `53`, `8000-9000`

#### firewall.forward_rules[].source

- Type: CIDR string
- Required: no

#### firewall.forward_rules[].dest

- Type: CIDR string
- Required: no

### firewall.input_rules

- Type: array
- Required: no

#### firewall.input_rules[].zone

- Type: string
- Required: yes
- Validation: must reference existing zone.

#### firewall.input_rules[].action

- Type: string
- Required: yes
- Allowed: `accept`, `drop`

#### firewall.input_rules[].protocol

- Type: string
- Required: no

#### firewall.input_rules[].port

- Type: string
- Required: no

#### firewall.input_rules[].source

- Type: CIDR string
- Required: no

## ecmp

### ecmp.enabled

- Type: boolean
- Required: yes when section is present
- Behavior: enables equal-cost default route rendering across WAN interfaces.

## pbr

- Type: array of policy rules
- Required: no

### pbr[].name

- Type: string
- Required: yes

### pbr[].priority

- Type: integer
- Required: yes
- Validation: must be unique across PBR rules.

### pbr[].source

- Type: CIDR string
- Required: no

### pbr[].interface

- Type: string
- Required: no
- Validation: if set, must reference an existing interface name.

## sysctl

### sysctl.conntrack_max

- Type: integer
- Required: no
- Default in renderer: `262144`

## Full Example

```yaml
hostname: edge-dc1
interfaces:
  - name: wan1
    role: wan
    device: eth0
    address: dhcp
    gateway: 100.64.0.1
    health_check:
      interval: 1
      timeout: 2
      failures: 3

  - name: wan2
    role: wan
    device: eth1
    address: dhcp
    gateway: 203.0.113.1

  - name: lan1
    role: lan
    device: eth2
    address: 192.168.10.1/24

  - name: guest100
    role: lan
    device: eth2.100
    address: 192.168.100.1/24
    vlan: 100

dhcp:
  enabled: true
  subnets:
    - subnet: 192.168.10.0/24
      interface: lan1
      pool_start: 192.168.10.100
      pool_end: 192.168.10.199
      gateway: 192.168.10.1
      dns_servers: [192.168.10.1]

dns:
  enabled: true
  forwarders: [1.1.1.1, 8.8.8.8]

firewall:
  zones:
    - name: lan
      interfaces: [lan1, guest100]
    - name: wan
      interfaces: [wan1, wan2]
  forward_rules:
    - from: lan
      to: wan
      action: accept
  input_rules:
    - zone: lan
      action: accept
      protocol: tcp
      port: "22"

ecmp:
  enabled: true

pbr:
  - name: guest-via-wan2
    priority: 100
    source: 192.168.100.0/24
    interface: wan2

sysctl:
  conntrack_max: 524288
```
