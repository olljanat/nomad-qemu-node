job "ubuntu" {
  group "vm" {
    count = 2
    restart {
      attempts = 0
    }
    update {
      max_parallel      = 1
      min_healthy_time  = "30s"
      healthy_deadline  = "15m"
      progress_deadline = "30m"
    }
    task "vm" {
      driver = "qemu"
      config {
        image_path   = "https://cloud-images.ubuntu.com/noble/20251213/noble-server-cloudimg-amd64.img"
        emulator     = "qemu-system-custom"
        machine_type = "q35"
        accelerator  = "kvm"
        args = [
          "-smp", "4"
          "-vlan", "1001"
        ]
        graceful_shutdown = true
      }
      kill_timeout = "5m"
      resources {
        cpu    = 2000  # Reserve 2 CPUs for VM, total CPU cores available for VM is set with "-smp" flag
        memory = 17408 # 16 GB + 1 GB for qemu-system-custom
      }
      template {
        data        = <<-EOF
#cloud-config
hostname: nomad-vm-{{ env "NOMAD_ALLOC_INDEX" }}
chpasswd:
  list:
  - root:P@ssw0rd!
  expire: false
users:
- name: ubuntu
  groups:
  - sudo
  sudo: ALL=(ALL) NOPASSWD:ALL
  ssh_authorized_keys:
  - ssh-ed25519 <removed>
  shell: /bin/bash
ssh_pwauth: true
runcmd:
- sed -i 's/GRUB_CMDLINE_LINUX=""/GRUB_CMDLINE_LINUX="vga=791"/' /etc/default/grub
- update-grub
- reboot
 EOF
        destination = "local/config-drive/openstack/latest/user_data"
      }
    }
  }
}