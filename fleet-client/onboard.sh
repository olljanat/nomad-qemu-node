#!/bin/bash
# Connect an existing Nomad QEMU node to BurFleet and upgrade it to a release
# which includes fleet-client. Works on the Debian 13 based releases too, with
# or without Secure Boot. Fill in the variables and paste to a root shell.
#
# The fleet-client configuration goes to /data/config/fleet-client.env, which
# is on the DATA partition, so it survives the upgrade and fleet-client starts
# on the first boot of the new image.

# --- Settings ----------------------------------------------------------------
FLEET_MANAGER_URL=https://fleet-manager.example.com
FLEET_GROUP=nomad-qemu
FLEET_ENVIRONMENT=production
# Only when the group + environment is protected with htpasswd
FLEET_USERNAME=
FLEET_PASSWORD=
# Nomad ACL token with node:write and namespace read access (see
# fleet-client/README.md), leave empty when ACLs are disabled
NOMAD_TOKEN=
# Release to upgrade to, see https://github.com/olljanat/nomad-qemu-node/releases
VERSION=v20261024-3
# true drains the node in Nomad before the reboot, fleet-client makes it
# eligible again after the boot
DRAIN=true
DRAIN_DEADLINE=1h
# -----------------------------------------------------------------------------

# Runs in a subshell, so an error does not close the shell it is pasted to
(
  set -euo pipefail
  [ "$(id -u)" = 0 ] || { echo "Run as root"; exit 1; }
  mountpoint -q /data || { echo "/data is not mounted"; exit 1; }

  # Use the TLS settings of the local Nomad agent when it has them
  if grep -qs '^\s*http\s*=\s*true' /data/config/security.hcl; then
    NOMAD_ADDR=https://localhost:4646
    NOMAD_CACERT=$(sed -n 's/^\s*ca_file\s*=\s*"\(.*\)"/\1/p' /data/config/security.hcl)
  else
    NOMAD_ADDR=http://localhost:4646
    NOMAD_CACERT=
  fi
  export NOMAD_ADDR NOMAD_CACERT NOMAD_TOKEN

  echo "Checking BurFleet at ${FLEET_MANAGER_URL}"
  curl -fsS -o /dev/null "${FLEET_MANAGER_URL%/}/healthz" \
    || { echo "BurFleet is not reachable"; exit 1; }
  echo "Checking local Nomad agent at ${NOMAD_ADDR}"
  nomad node status -self > /dev/null \
    || { echo "Nomad token does not work"; exit 1; }

  echo "Writing /data/config/fleet-client.env"
  mkdir -p /data/config
  umask 077
  {
    echo "FLEET_MANAGER_URL=${FLEET_MANAGER_URL}"
    echo "FLEET_GROUP=${FLEET_GROUP}"
    echo "FLEET_ENVIRONMENT=${FLEET_ENVIRONMENT}"
    [ -n "${FLEET_USERNAME}" ] && echo "FLEET_USERNAME=${FLEET_USERNAME}"
    [ -n "${FLEET_PASSWORD}" ] && echo "FLEET_PASSWORD=${FLEET_PASSWORD}"
    echo "NOMAD_ADDR=${NOMAD_ADDR}"
    [ -n "${NOMAD_CACERT}" ] && echo "NOMAD_CACERT=${NOMAD_CACERT}"
    [ -n "${NOMAD_TOKEN}" ] && echo "NOMAD_TOKEN=${NOMAD_TOKEN}"
    echo "FLEET_NOMAD_DRAIN_DEADLINE=${DRAIN_DEADLINE}"
  } > /data/config/fleet-client.env
  chmod 600 /data/config/fleet-client.env

  if [ "${DRAIN}" = true ]; then
    echo "Draining the node in Nomad"
    # fleet-client makes the node eligible again when this marker exists
    mkdir -p /data/fleet-client
    touch /data/fleet-client/drained
    nomad node drain -self -enable -yes -ignore-system -deadline "${DRAIN_DEADLINE}"
  fi

  echo "Upgrading to ${VERSION}"
  mount -o remount,rw /.snapshots
  # --bootloader replaces shim and GRUB, so Secure Boot accepts the new kernel
  if ! elemental upgrade --bootloader --system "oci:ghcr.io/olljanat/nomad-qemu-node:${VERSION}"; then
    echo "Upgrade failed, the node stays on the current version"
    if [ "${DRAIN}" = true ]; then
      nomad node drain -self -disable -yes
      rm -f /data/fleet-client/drained
    fi
    exit 1
  fi
  echo "Upgrade done, rebooting"
  reboot
)
