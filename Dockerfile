# OS base image of our choice
FROM debian:13 AS os

# install kernel, systemd, dracut, grub2 and other required tools
RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    apparmor \
    bash-completion \
    bridge-utils \
    bsdextrautils \
    btrfsmaintenance \
    btrfs-progs \
    ca-certificates \
    curl \
    dbus-daemon \
    dmsetup \
    dosfstools \
    dracut-core \
    dracut-live \
    dracut-network \
    dracut-squash \
    e2fsprogs \
    eject \
    findutils \
    fdisk \
    gdisk \
    genisoimage \
    gpg \
    grub2-common \
    grub-efi-amd64-signed \
    haveged \
    htop \
    iproute2 \
    iptables \
    iputils-ping \
    kbd \
    kmod \
    less \
    linux-image-amd64 \
    lldpd \
    locales \
    lvm2 \
    netcat-traditional \
    mdadm \
    mtools \
    netplan.io \
    net-tools \
    networkd-dispatcher \
    openssh-client \
    openssh-server \
    parted \
    patch \
    psmisc \
    rsync \
    shim-signed \
    socat \
    squashfs-tools \
    systemd \
    systemd-resolved \
    systemd-sysv \
    systemd-timesyncd \
    tcpdump \
    tzdata \
    vim \
    wget \
    xorriso \
    xz-utils \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/* \
    && echo > /etc/motd

# Hack to prevent systemd-firstboot failures while setting keymap, this is known
# Debian issue (T_T) https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=790955
ARG KBD=2.6.4
RUN curl -L https://mirrors.edge.kernel.org/pub/linux/utils/kbd/kbd-${KBD}.tar.xz --output kbd-${KBD}.tar.xz \
    && tar xaf kbd-${KBD}.tar.xz \
    && mkdir -p /usr/share/keymaps \
    && cp -Rp kbd-${KBD}/data/keymaps/* /usr/share/keymaps/ \
    && rm kbd-${KBD}.tar.xz \
    && mkdir /data

# Add module configs
COPY /modprobe.d/* /etc/modprobe.d/

# Disable audit message spam to console
RUN systemctl mask systemd-journald-audit.socket

# Configure lldpd to interface name for switches
RUN echo 'configure lldp portidsubtype ifname' > /etc/lldpd.d/port_info.conf

# Symlink grub2-editenv
RUN ln -sf /usr/bin/grub-editenv /usr/bin/grub2-editenv

# Just add the elemental cli
COPY /elemental /usr/bin/elemental

# Enable essential services
RUN systemctl enable systemd-networkd.service

# Generate en_US.UTF-8 locale, this the locale set at boot by
# the default cloud-init
RUN locale-gen --lang en_US.UTF-8

# Hide some useless default infos on login
RUN rm -f /etc/update-motd.d/10-help-text \
    && rm -f /etc/update-motd.d/50-motd-news \
    && rm -f /etc/update-motd.d/60-unminimize

# Add default snapshotter setup
COPY config/snapshotter.yaml /etc/elemental/config.d/snapshotter.yaml

# Add configuration
COPY config/config.yaml /etc/elemental/

# Generate initrd with required elemental services
RUN elemental --debug init -f

# Store version number
ARG VERSION
ENV VERSION=${VERSION}
RUN echo IMAGE_TAG=\"${VERSION}\" >> /etc/os-release

# Branding
COPY /config/oem/ /system/oem/

# Arrange bootloader binaries into /usr/lib/elemental/bootloader
# this way elemental installer can easily fetch them
RUN mkdir -p /usr/lib/elemental/bootloader && \
    cp /usr/lib/grub/x86_64-efi-signed/grubx64.efi.signed /usr/lib/elemental/bootloader/grubx64.efi && \
    cp /usr/lib/shim/shimx64.efi.signed /usr/lib/elemental/bootloader/shimx64.efi && \
    cp /usr/lib/shim/mmx64.efi /usr/lib/elemental/bootloader/mmx64.efi

# Add QEMU
COPY /scripts/* /usr/local/bin/
COPY /vm-console/qemu-vm-console /usr/local/bin/
RUN apt-get update \
    && apt-get install -y novnc qemu-system-x86 \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# Add HashiCorp Nomad
ARG NOMAD_VERSION=unknown
RUN wget -O- https://apt.releases.hashicorp.com/gpg | \
    gpg --dearmor > /usr/share/keyrings/hashicorp-archive-keyring.gpg \
    && echo "deb [arch=amd64 signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com trixie main" > /etc/apt/sources.list.d/hashicorp.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends nomad=${NOMAD_VERSION} \
    && rm -rf /etc/nomad.d \
    && rm -rf /opt/nomad \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*
COPY /config/nomad.d/* /usr/share/nomad/

# Make sure that /data is mounted before starting Nomad
COPY /systemd/* /usr/lib/systemd/system/
RUN systemctl enable data.mount \
    && systemctl enable nomad.service \
    && systemctl enable qemu-ga-server.service \
    && systemctl enable qemu-vm-console.service

# Ensure that every server has unique machine-id and bridge interface MAC address
# https://wiki.debian.org/MachineId
# https://fedoraproject.org/wiki/Changes/MAC_Address_Policy_none
COPY /config/99-default.link /lib/systemd/network/
RUN rm -f /var/lib/dbus/machine-id \
    && ln -s /etc/machine-id /var/lib/dbus/machine-id \
    && rm -f /etc/machine-id \

# Good for validation after the build
CMD ["/bin/bash"]
