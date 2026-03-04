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
