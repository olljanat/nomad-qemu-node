# Nomad QEMU node
Minimal OS to run QEMU VMs and orchestrate them with HashiCorp Nomad.

## Design principles
### Targets
* Keep It Simple Stupid (there is enough complex hypervisor/HCI solutions for complex workloads).
* Maintenance free OS by building it with [Elemental toolkit](https://rancher.github.io/elemental-toolkit/).
* Running _compute only_ VMs with minimal cost in any server hardware.
* Automatic node networking (all NICs included to bond, getting IP from DHCP).
* Automatic VM image download and removal.
* VM isolation with VLANs.
* Web based VM console access.
* Run all VMs in UEFI mode with secure boot.

### Not targets
* Running VMs with persistent storage.

# Usage
## Install
1. Boot from ISO (username: `root` , password: `elemental`)
2. Install by running command: `elemental install /dev/sda`
3. Format and mount data drive `mkfs.ext4 -L DATA /dev/sdX && mount LABEL=DATA /data`
4. Copy example configs `mkdir /data/config && cp /usr/share/nomad/* /data/config/`
5. Configure Nomad by either:
     - Create/import [Nomad TLS certificates](https://developer.hashicorp.com/nomad/docs/secure/traffic/tls) and [bootstrap ACLs](https://developer.hashicorp.com/nomad/docs/secure/acl/bootstrap) (or disable security by removing `/data/config/security.hcl`)
     - Connect to remote Nomad server by removing `/data/config/server.hcl` and correcting server names to `/data/config/client.hcl` (or run single node cluster by removing `/data/config/client.hcl`)
6. Reboot from disk.
7. Deploy job from [examples](/examples).
8. Connect to http://<server IP>:5800 to connect VM console.

# Upgrade
1. Run command `elemental upgrade --reboot`
2. Check version by running `grep IMAGE_TAG /etc/os-release`
