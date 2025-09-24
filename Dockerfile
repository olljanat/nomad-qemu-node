# OS base image of our choice
FROM ubuntu:24.04 AS os

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
    gpg \
    grub2-common \
    grub-efi-amd64 \
    haveged \
    htop \
    iproute2 \
    iputils-ping \
    kbd \
    kmod \
    less \
    linux-generic \
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
    shim \
    shim-signed \
    socat \
    squashfs-tools \
    systemd \
    systemd-resolved \
    systemd-sysv \
    systemd-timesyncd \
    tcpdump \
    tzdata \
    uml-utilities \
    vim \
    wget \
    xorriso \
    xz-utils \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# Hack to prevent systemd-firstboot failures while setting keymap, this is known
# Debian issue (T_T) https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=790955
ARG KBD=2.6.4
RUN curl -L https://mirrors.edge.kernel.org/pub/linux/utils/kbd/kbd-${KBD}.tar.xz --output kbd-${KBD}.tar.xz \
    && tar xaf kbd-${KBD}.tar.xz \
    && mkdir -p /usr/share/keymaps \
    && cp -Rp kbd-${KBD}/data/keymaps/* /usr/share/keymaps/ \
    && rm kbd-${KBD}.tar.xz \
    && mkdir /data

# Remove default user
RUN userdel ubuntu \
    && rm -rf /home/ubuntu

# Symlink grub2-editenv
RUN ln -sf /usr/bin/grub-editenv /usr/bin/grub2-editenv

# Just add the elemental cli
COPY /elemental /usr/bin/elemental

# Enable essential services
RUN systemctl enable systemd-networkd.service

# Enable /tmp to be on tmpfs
RUN cp /usr/share/systemd/tmp.mount /etc/systemd/system

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
    cp /usr/lib/shim/shimx64.efi.signed.latest /usr/lib/elemental/bootloader/shimx64.efi && \
    cp /usr/lib/shim/mmx64.efi /usr/lib/elemental/bootloader/mmx64.efi

# Add QEMU
ARG QEMU_VERSION=unknown
RUN apt-get update \
    && apt-get install -y qemu-system-x86=${QEMU_VERSION} \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*
COPY /scripts/qemu-system-custom /usr/local/bin/

# Add HashiCorp Nomad
ARG NOMAD_VERSION=unknown
RUN wget -O- https://apt.releases.hashicorp.com/gpg | \
    gpg --dearmor > /usr/share/keyrings/hashicorp-archive-keyring.gpg \
    && echo "deb [arch=amd64 signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com noble main" > /etc/apt/sources.list.d/hashicorp.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends nomad=${NOMAD_VERSION} \
    && systemctl enable nomad.service \
    && rm -rf /etc/nomad.d \
    && rm -rf /opt/nomad \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*
COPY /config/nomad.service /usr/lib/systemd/system/
COPY /config/nomad.d/* /usr/share/nomad/

# Good for validation after the build
CMD ["/bin/bash"]
