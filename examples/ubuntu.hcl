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
        image_path   = "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img"
        emulator     = "qemu-system-custom"
        machine_type = "q35"
        accelerator  = "kvm"
        args = [
          "-mem-min", "2048", # Enable dynamic memory between this and max memory configured in "resources" block
          "-smp", "4",
          "-vlan", "1001"
        ]
        graceful_shutdown = true
        guest_agent = true
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
packages:
- qemu-guest-agent
runcmd:
- sed -i 's/GRUB_CMDLINE_LINUX=""/GRUB_CMDLINE_LINUX="vga=791"/' /etc/default/grub
- update-grub
- reboot
 EOF
        destination = "local/config-drive/openstack/latest/user_data"
      }
      service {
        name     = "ubuntu-vm-qemu-agent"
        provider = "nomad"
        address  = "127.0.0.1"
        port     = "qemu_guest_agent"
        check {
          name     = "ping"
          type     = "http"
          path     = "/qga/${NOMAD_ALLOC_ID}/vm/guest-ping"
          interval = "1m"
          timeout  = "1s"
        }
      }
    }
    network {
      port "qemu_guest_agent" {}
    }
  }
}
