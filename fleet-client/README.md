# Fleet client

System service which connects a Nomad QEMU node to
[BurFleet](https://github.com/burmilla/os-fleet-manager), so the OS upgrades
and scheduled restarts of all nodes are controlled from one place. It is a
modified version of the BurmillaOS
[fleet-client](https://github.com/burmilla/os-services/tree/v3.0.x/images/20-fleetclient).

On every check interval (from BurFleet, 5 minutes by default) it:

1. Reports the node state to BurFleet: hostname, group, environment, OS
   version (`IMAGE_TAG` in `/etc/os-release`), kernel, QEMU and Nomad
   versions, boot time and the status of the local Nomad client node
   (status, scheduling eligibility, drain and the number of running
   allocations) and the software RAID arrays from `/proc/mdstat` (level,
   state, expected / in-sync devices, failed and spare devices and a
   running rebuild, resync or check with its progress). The report says
   `node_type: nomad-qemu-node`, so BurFleet does not apply BurmillaOS
   settings to the node.
2. Gets back the target OS image and version, upgrade policy, maintenance
   window and whether a scheduled restart is due.
3. Depending on `upgrade_policy`:
   * `none` or `download` – only reports (elemental downloads the image
     during the upgrade, so there is nothing to pre-pull).
   * `auto` – inside the maintenance window, asks BurFleet for a
     **maintenance lock**. With the lock it
     1. drains the node in Nomad (`nomad node drain -self -enable
        -ignore-system`), so the VMs are rescheduled to other nodes, and
        waits until the drain is done (`FLEET_NOMAD_DRAIN_DEADLINE`),
     2. upgrades with `elemental upgrade --bootloader --system
        oci:<os_image>:<os_version>` (OS upgrade; `--bootloader` keeps the
        Secure Boot shim and GRUB in sync with the new image) and reboots,
        or just reboots (scheduled restart),
     3. after the boot makes the node eligible for scheduling again.

     BurFleet releases the lock once the node reports the target version
     and its Nomad node is `ready`, eligible and not draining, so the next
     node starts only after the previous one is back. A lock is only given
     while the other Nomad nodes of the group are up, eligible and not
     draining. If the upgrade fails the node is made eligible again,
     reports `error` and retries after an hour.

   With `none` and `download`, scheduled restarts still run the same way
   (lock, drain, reboot) when BurFleet allows them (`maintenance_window`
   and `max_uptime` configured, BurFleet v1.5.0 or later). OS upgrades
   stay pending.
4. Applies the `ssh` settings right away, in any upgrade policy and without
   a maintenance lock:
   * `authorized_keys` go to a block between `# BEGIN burmilla-fleet-manager`
     and `# END burmilla-fleet-manager` in `/root/.ssh/authorized_keys`.
     `/root/.ssh` is made `root:root` 0700, otherwise sshd ignores the keys.
     Other keys in the file are not touched.
   * `password_authentication` is written to
     `/etc/ssh/sshd_config.d/50-burmilla-fleet.conf` (with `false` also
     `KbdInteractiveAuthentication no`), checked with `sshd -t` and sshd is
     reloaded; open sessions stay. The effective value (`sshd -T`) is
     reported back. Console logins with the root password keep working.

   `/root` and `/etc/ssh` are persistent, so the settings survive upgrades.
   Make sure the keys work before disabling password logins.

Requires BurFleet with Nomad QEMU node support, v1.3.0 or newer to see the
software RAID status (older versions ignore it).

## Enable

1. Create a Nomad ACL policy and a token for the client (skip when ACLs are
   disabled):
   ```bash
   cat > fleet-client.hcl <<EOF
   node {
     policy = "write"
   }
   namespace "*" {
     policy = "read"
   }
   EOF
   nomad acl policy apply -description "BurFleet client" fleet-client fleet-client.hcl
   nomad acl token create -name fleet-client -policy fleet-client
   ```
2. Copy the example configuration and fill it in:
   ```bash
   cp /usr/share/fleet-client/fleet-client.env.example /data/config/fleet-client.env
   chmod 600 /data/config/fleet-client.env
   vi /data/config/fleet-client.env
   systemctl restart fleet-client
   ```
3. Follow the logs with `journalctl -u fleet-client -f`.

The service only starts when `/data/config/fleet-client.env` exists.

### Existing installations

Nodes installed from an older release (also the Debian 13 based ones, with or
without Secure Boot) can be connected and upgraded in one go with
[onboard.sh](onboard.sh). Create the Nomad token of step 1 once, fill in the
settings at the top of the script and paste it to a root shell of each node.
It checks that BurFleet and the Nomad token work, writes
`/data/config/fleet-client.env`, drains the node in Nomad, upgrades with
`elemental upgrade --bootloader` and reboots. fleet-client starts on the
first boot of the new image and makes the node eligible again.

| Variable                     | Default              | Description |
|------------------------------|----------------------|-------------|
| `FLEET_MANAGER_URL`          |                      | BurFleet address (required) |
| `FLEET_GROUP`                |                      | Group of the node in BurFleet (a `standalone: true` entry under `swarms:`). `FLEET_SWARM` works too |
| `FLEET_ENVIRONMENT`          |                      | Environment of the node, set together with `FLEET_GROUP` |
| `FLEET_USERNAME`             |                      | User of the group + environment when it is protected with `htpasswd` |
| `FLEET_PASSWORD`             |                      | Password for `FLEET_USERNAME` |
| `FLEET_CA_CERT`              |                      | Extra CA certificate (PEM) to trust for an HTTPS BurFleet |
| `FLEET_HOSTNAME`             | system hostname      | Identity of the node in the fleet |
| `FLEET_INTERVAL`             | from BurFleet        | Override the check interval (Go duration) |
| `FLEET_NOMAD_DRAIN`          | `true`               | `false` skips the Nomad drain before maintenance |
| `FLEET_NOMAD_DRAIN_DEADLINE` | `1h`                 | Deadline of the drain, after which remaining allocations are stopped |
| `FLEET_UPGRADE_ARGS`         |                      | Extra `elemental upgrade` arguments |
| `FLEET_STATE_DIR`            | `/data/fleet-client` | Persistent directory for the drain marker |
| `NOMAD_ADDR`, `NOMAD_CACERT`, `NOMAD_TOKEN`, ... | | Used by the `nomad` CLI to reach the local agent |

## BurFleet configuration

Put the nodes in their own standalone group and set the image there.
`os_version` is a tag of `os_image`, i.e. a release of this repository:

```yaml
swarms:
  nomad-qemu:
    standalone: true
    os_image: ghcr.io/olljanat/nomad-qemu-node
    os_version: v20261024-1
    upgrade_policy: auto
    max_uptime: 90d
    maintenance_window:
      days: [sat]
      start: "02:00"
      duration: 4h
      timezone: Europe/Helsinki
    environments:
      production: {}
```

The BurmillaOS settings of `defaults` (`os_image`, `os_version`) and all
`docker_version` settings do not apply to Nomad QEMU nodes. `ssh` settings
are inherited like on BurmillaOS (`defaults` included) and apply to root:

```yaml
swarms:
  nomad-qemu:
    ssh:
      authorized_keys:
        - ssh-ed25519 AAAA... alice@example.com
      password_authentication: false
```

Run fleet-client on all Nomad client nodes of the group: a node that stops
reporting, reports an error or is not eligible blocks maintenance of the
others.
