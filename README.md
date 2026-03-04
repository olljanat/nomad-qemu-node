# Nomad QEMU node
Minimal OS to run QEMU VMs and orchestrate them with HashiCorp Nomad.

## Design principles
### Targets
* Keep It Simple Stupid (there is enough complex hypervisor/HCI solutions for complex workloads).
* Maintenance free OS by building it with [Elemental toolkit](https://rancher.github.io/elemental-toolkit/).
* Running large amount of identical, _compute only_ VMs with minimal cost in any server hardware.
* Automatic node networking (all NICs included to bond, getting IP from DHCP).
* Automatic VM image download and removal.
* VM isolation with VLANs.
* Web based VM console access.
* Run all VMs in UEFI mode with secure boot.
* Ensure compatibility with Windows VMs.
* Minimize host memory usage by supporting VMs memory dynamic scaling with virtio-mem driver.

### Not targets
* Running VMs with persistent storage.

# Usage
## Install
1. Boot from ISO (username: `root` , password: `elemental`)
2. Create IP reservation to DHCP and set hostname (option 12).
3. Create mirrored RAID volume for OS (**NOTE** uuid must be exactly that):
```bash
mdadm --create --verbose /dev/md0 \
  --level=1 \
  --raid-devices=2 \
  --bitmap=internal \
  --metadata=1.0 \
  --uuid=e0979e44:ad38:4165:9aa2:cf2dd13b0de7 \
  /dev/sda /dev/sdb
```
4. Wait mirroring to be ready: `mdadm --wait /dev/md0`
5. Install by running command: `elemental install /dev/md0`
6. Format and mount data drive `mkfs.ext4 -L DATA /dev/sdX && mount LABEL=DATA /data`
7. Copy example configs `mkdir /data/config && cp /usr/share/nomad/* /data/config/`
8. Configure Nomad by either:
     - Create/import [Nomad TLS certificates](https://developer.hashicorp.com/nomad/docs/secure/traffic/tls) and [bootstrap ACLs](https://developer.hashicorp.com/nomad/docs/secure/acl/bootstrap) (or disable security by removing `/data/config/security.hcl`)
     - Connect to remote Nomad server by removing `/data/config/server.hcl` and correcting server names to `/data/config/client.hcl` (or run single node cluster by removing `/data/config/client.hcl`)
9. Reboot from disk.
10. Deploy job from [examples](/examples).
11. Connect to `http://<server IP>:5800` to connect VM console.

# Upgrade
1. Run command `mount -o remount,rw /.snapshots && elemental upgrade --reboot`
2. Check version by running `grep IMAGE_TAG /etc/os-release`

# Monitoring
Following monitoring/troubleshooting tools are included to media:
* `htop`
* `perf kvm stat live`
* `mdadm --detail /dev/md0`

In additionally we enable Nomad [raw_exec](https://developer.hashicorp.com/nomad/docs/deploy/task-driver/raw_exec) driver which allow you to deploy extra tools.
