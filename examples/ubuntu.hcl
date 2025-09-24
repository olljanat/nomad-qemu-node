job "ubuntu" {
  group "group" {
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
          "-vlan", "3461",
          "-vnc", ":${NOMAD_ALLOC_INDEX}"
        ]
        graceful_shutdown = true
      }
      kill_timeout = "5m"
      resources {
        memory = 8192
        cores  = 4
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
- name: rancher
  groups:
  - sudo
  sudo: ALL=(ALL) NOPASSWD:ALL
  ssh_authorized_keys:
  - ssh-ed25519 <removed>
  shell: /bin/bash
ssh_pwauth: true
 EOF
        destination = "local/config-drive/openstack/latest/user_data"
      }
    }
  }
}