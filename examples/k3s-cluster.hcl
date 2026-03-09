# Example of K3s high-availability cluster where OS disk is stateless but Kubernetes data persistent in /data
# Additionally this example uses PCI passthrough for SCSI controller which allow storage cluster
# to be hosted inside of Kubernetes.
#
#### Preparation:
# Find device IDs: lspci -nnk | grep "SCSI"
# Add kernel parameters which make sure that those are available as /dev/vfio/* instead of reserved by host OS:
# grub2-editenv /oem/grubenv set extra_cmdline="modprobe.blacklist=mpt3sas vfio-pci.ids=1000:0097"
#
# Create persistent disk for each node with command like:
# qemu-img create -f qcow2 k3s-s1-data.qcow2 100G
# and adjust -drive parameter matching to that.
#
# Create Nomad variable "nomad/jobs/k3s" with following values:
# * "K3sNodePwdSuffix" which should be random combination of letters and numbers without special characters
# * "K3sToken" copies from first node file /data/k3s/server/token
#
job "k3s" {
  group "s1" {
    constraint {
      attribute = "${attr.unique.hostname}"
      value = "qemu1"
    }
    count = 1
    update {
      max_parallel      = 1
      min_healthy_time  = "1s"
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
          "-image-resize", "50G", # Extend OS disk to 50GB
          "-mem-min", "4092", # Enable dynamic memory between this and max memory configured in "resources" block
          "-smp", "4",
          "-vlan", "502",
          # Persistent /data disk
          "-drive", "file=/data/persistent/k3s-s1-data.qcow2,if=none,id=image1,format=qcow2",
          "-device", "scsi-hd,drive=image1,bus=scsi0.0,lun=1",
          # Passthrough SCSI controllers with all the disks
          "-device", "vfio-pci,host=0000:d8:00.0",
          "-device", "vfio-pci,host=0000:d9:00.0"
        ]
        graceful_shutdown = true
        guest_agent = true
      }
      kill_timeout = "5m"
      resources {
        cpu    = 2000  # Reserve 2 CPUs for VM, total CPU cores available for VM is set with "-smp" flag
        memory = 9216 # 8 GB + 1 GB for qemu-system-custom
      }
      template {
        data = <<-EOF
{{ with nomadVar `nomad/jobs/k3s` }}
#cloud-config
hostname: k3s-s1
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
write_files:
- path: /etc/netplan/50-cloud-init.yaml
  group: 0
  owner: 0
  permissions: 0600
  content: |
    network:
      version: 2
      ethernets:
        enp0s3:
          dhcp4: true
          addresses:
          - 10.5.2.11/24
          nameservers:
            addresses:
            - 1.1.1.1
            search: []
          routes:
          - to: default
            via: 10.5.2.1
- path: /etc/rancher/k3s/config.yaml
  group: 0
  owner: 0
  permissions: 0600
  content: |
    write-kubeconfig-mode: "0644"
    tls-san:
    - "k3s.lan"
    cluster-init: true
runcmd:
- |
    netplan apply
    e2fsck -y /dev/sdb1
    mkdir -p /data
    mount /dev/sdb1 /data
    mkdir -p /etc/rancher/node
    echo "k3s-s1-{{.K3sNodePwdSuffix}}" > /etc/rancher/node/password
    curl -sfL https://get.k3s.io | \
      INSTALL_K3S_VERSION=v1.34.4+k3s1 \
      INSTALL_K3S_EXEC="server --data-dir /data/k3s" \
      sh -
disk_setup:
  /dev/sdb:
    layout: true
    table_type: gpt
fs_setup:
- label: DATA
  partition: auto
  filesystem: ext4
  device: /dev/sdb
growpart:
  devices:
  - /dev/sda1
  - /dev/sdb1
  mode: auto
{{ end }}
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
  group "s2" {
    constraint {
      attribute = "${attr.unique.hostname}"
      value = "qemu2"
    }
    count = 1
    update {
      max_parallel      = 1
      min_healthy_time  = "1s"
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
          "-image-resize", "50G", # Extend OS disk to 50GB
          "-mem-min", "4092", # Enable dynamic memory between this and max memory configured in "resources" block
          "-smp", "4",
          "-vlan", "502",
          # Persistent /data disk
          "-drive", "file=/data/persistent/k3s-s2-data.qcow2,if=none,id=image1,format=qcow2",
          "-device", "scsi-hd,drive=image1,bus=scsi0.0,lun=1",
          # Passthrough SCSI controllers with all the disks
          "-device", "vfio-pci,host=0000:d8:00.0",
          "-device", "vfio-pci,host=0000:d9:00.0"
        ]
        graceful_shutdown = true
        guest_agent = true
      }
      kill_timeout = "5m"
      resources {
        cpu    = 2000  # Reserve 2 CPUs for VM, total CPU cores available for VM is set with "-smp" flag
        memory = 9216 # 8 GB + 1 GB for qemu-system-custom
      }
      template {
        data = <<-EOF
{{ with nomadVar `nomad/jobs/k3s` }}
#cloud-config
hostname: k3s-s2
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
write_files:
- path: /etc/netplan/50-cloud-init.yaml
  group: 0
  owner: 0
  permissions: 0600
  content: |
    network:
      version: 2
      ethernets:
        enp0s3:
          dhcp4: true
          addresses:
          - 10.5.2.12/24
          nameservers:
            addresses:
            - 1.1.1.1
            search: []
          routes:
          - to: default
            via: 10.5.2.1
runcmd:
- |
    netplan apply
    e2fsck -y /dev/sdb1
    mkdir -p /data
    mount /dev/sdb1 /data
    echo "Waiting s1 to be ready"
    sleep 2m
    mkdir -p /etc/rancher/node
    echo "k3s-s2-{{.K3sNodePwdSuffix}}" > /etc/rancher/node/password
    curl -sfL https://get.k3s.io | \
      INSTALL_K3S_VERSION=v1.34.4+k3s1 \
      K3S_TOKEN="{{.K3sToken}}" \
      K3S_URL="https://10.5.2.11:6443" \
      INSTALL_K3S_EXEC="server --data-dir /data/k3s" \
      sh -
disk_setup:
  /dev/sdb:
    layout: true
    table_type: gpt
fs_setup:
- label: DATA
  partition: auto
  filesystem: ext4
  device: /dev/sdb
growpart:
  devices:
  - /dev/sda1
  - /dev/sdb1
  mode: auto
{{ end }}
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
  group "s3" {
    constraint {
      attribute = "${attr.unique.hostname}"
      value = "qemu3"
    }
    count = 1
    update {
      max_parallel      = 1
      min_healthy_time  = "1s"
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
          "-image-resize", "50G", # Extend OS disk to 50GB
          "-mem-min", "4092", # Enable dynamic memory between this and max memory configured in "resources" block
          "-smp", "4",
          "-vlan", "502",
          # Persistent /data disk
          "-drive", "file=/data/persistent/k3s-s3-data.qcow2,if=none,id=image1,format=qcow2",
          "-device", "scsi-hd,drive=image1,bus=scsi0.0,lun=1",
          # Passthrough SCSI controllers with all the disks
          "-device", "vfio-pci,host=0000:d8:00.0",
          "-device", "vfio-pci,host=0000:d9:00.0"
        ]
        graceful_shutdown = true
        guest_agent = true
      }
      kill_timeout = "5m"
      resources {
        cpu    = 2000  # Reserve 2 CPUs for VM, total CPU cores available for VM is set with "-smp" flag
        memory = 9216 # 8 GB + 1 GB for qemu-system-custom
      }
      template {
        data = <<-EOF
{{ with nomadVar `nomad/jobs/k3s` }}
#cloud-config
hostname: k3s-s3
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
write_files:
- path: /etc/netplan/50-cloud-init.yaml
  group: 0
  owner: 0
  permissions: 0600
  content: |
    network:
      version: 2
      ethernets:
        enp0s3:
          dhcp4: true
          addresses:
          - 10.5.2.13/24
          nameservers:
            addresses:
            - 1.1.1.1
            search: []
          routes:
          - to: default
            via: 10.5.2.1
runcmd:
- |
    netplan apply
    e2fsck -y /dev/sdb1
    mkdir -p /data
    mount /dev/sdb1 /data
    echo "Waiting s1 to be ready"
    sleep 2m
    mkdir -p /etc/rancher/node
    echo "k3s-s3-{{.K3sNodePwdSuffix}}" > /etc/rancher/node/password
    curl -sfL https://get.k3s.io | \
      INSTALL_K3S_VERSION=v1.34.4+k3s1 \
      K3S_TOKEN="{{.K3sToken}}" \
      K3S_URL="https://10.5.2.11:6443" \
      INSTALL_K3S_EXEC="server --data-dir /data/k3s" \
      sh -
disk_setup:
  /dev/sdb:
    layout: true
    table_type: gpt
fs_setup:
- label: DATA
  partition: auto
  filesystem: ext4
  device: /dev/sdb
growpart:
  devices:
  - /dev/sda1
  - /dev/sdb1
  mode: auto
{{ end }}
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