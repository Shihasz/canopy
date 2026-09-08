#!/bin/bash
# Runs once on first boot (GCP's "startup-script" metadata mechanism).
# Installs dependencies and lays down the same directory/user structure
# as the local docker-compose lab (lab/vm/Dockerfile), so canopy's
# systemd deploy/rollback logic works identically against either target.
set -euo pipefail

apt-get update
apt-get install -y --no-install-recommends dbus

# The 'deploy' user is who canopy connects as over SSH. The startup
# script metadata already provisions its SSH key via GCP's own
# OS Login / metadata SSH keys mechanism (see instance config), so we
# only need to create the user and sudo rule here.
useradd -m -s /bin/bash deploy || true
echo "deploy ALL=(ALL) NOPASSWD: /usr/bin/tee, /usr/bin/systemctl" > /etc/sudoers.d/deploy
chmod 440 /etc/sudoers.d/deploy

useradd -m -s /usr/sbin/nologin appuser || true

mkdir -p /opt/checkout-svc/releases
mkdir -p /etc/canopy
echo "${TARGET_LABEL}" > /etc/canopy/target-label

# NOTE: this script provisions the VM shell (users, directories, systemd
# readiness) but deliberately does NOT install an application binary or
# release versions — that's the same artifact-distribution boundary
# described in the local lab (see lab/README or main README): shipping
# app releases is a separate concern (CI artifact upload, rsync, image
# baking) from what canopy itself orchestrates. Populate
# /opt/checkout-svc/releases/<version>/app yourself before running
# `canopy deploy` against these VMs.
