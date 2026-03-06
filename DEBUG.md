# Debug
## Boot issues
* Add following parameter to kernel command line in grub: `rd.break=pre-pivot rd.shell=1 SYSTEMD_SULOGIN_FORCE=1`
* Chroot to system by running these commands:
```bash
mount --bind /dev /sysroot/dev
mount --bind /proc /sysroot/proc
mount --bind /sys /sysroot/sys
mount --bind /run /sysroot/run
/sysroot/usr/sbin/chroot /sysroot
```
* See boot logs with command: `journalctl -xe`

Look: https://rancher.github.io/elemental-toolkit/docs/reference/troubleshooting/#debug-initramfs-issues

## Reinstall
> [!CAUTION]
> These commands will remove all existing data from system.
```bash
systemctl stop nomad
umount /data
mdadm --stop /dev/mdX # Repeat for all md devices
blkdeactivate
wipefs -a /dev/sdX # Repeat for all disks
```

## PCI passthrough without hardware support
If your server does not support [IRQ remapping](https://pve.proxmox.com/wiki/PCI_Passthrough#Verify_IOMMU_interrupt_remapping_is_enabled). You can allow unsafe interrupts with these commands:
```bash
rmmod vfio_iommu_type1
modprobe vfio_iommu_type1 allow_unsafe_interrupts=1
cat /sys/module/vfio_iommu_type1/parameters/allow_unsafe_interrupts
```
