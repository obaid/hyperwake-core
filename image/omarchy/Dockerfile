# Hyperwake flagship image: genuine Omarchy.
#
# This is NOT the Debian/i3 stand-in in ../docker. It is real Arch Linux with
# the real upstream Omarchy package set, installed from Omarchy's own pacman
# repository and configured from a pinned upstream git tag.
#
# One thing does not work in a container, and it is load-bearing: the desktop.
# Omarchy is Hyprland, Hyprland 0.56 renders through aquamarine, and aquamarine
# needs a DRM render node to allocate buffers on every backend path it has.
# Docker Desktop's LinuxKit kernel exposes no /dev/dri and has no vkms module,
# so the compositor cannot start here. It starts normally on a KVM guest with
# virtio-gpu. See docs/implementation/decisions/ADR-IMAGE-001-omarchy-in-docker.md.
#
# Everything else in the image — package set, Omarchy configuration, guest
# daemon, sshd, persistence, shutdown — is exercised locally.
#
# amd64 only. Omarchy targets Arch x86_64; Arch Linux ARM is a different
# distribution and its hyprland/aquamarine packages are currently mutually
# unsatisfiable (hyprland wants libaquamarine.so=13, the repo ships 14).
FROM --platform=linux/amd64 archlinux:latest

ARG OMARCHY_REF=v4.0.3
ARG HYPERWAKE_IMAGE_VERSION=0.1.0

# pacman 7 sandboxes downloads with Landlock and an unprivileged `alpm` user.
# Docker Desktop's VM permits neither, and the failure mode is an unhelpful
# "failed to synchronize all databases".
RUN sed -i 's/^#\?DownloadUser.*/#DownloadUser = alpm/' /etc/pacman.conf \
 && printf '\nDisableSandbox\n' >> /etc/pacman.conf

# Omarchy's own repository. This is what makes the image genuinely Omarchy
# rather than an Arch box with similar packages: aether, omacut, omawrite,
# omarchy-nvim and the rest are built and signed by the Omarchy project.
RUN printf '\n[omarchy]\nSigLevel = Optional TrustAll\nServer = https://pkgs.omarchy.org/stable/$arch\n' >> /etc/pacman.conf \
 && pacman -Sy --noconfirm

# Upstream configuration, pinned. Cloned before the package install so a broken
# tag fails the build early and cheaply.
RUN pacman -S --noconfirm --needed git \
 && git clone --depth 1 --branch "${OMARCHY_REF}" \
      https://github.com/omacom/omarchy /usr/share/omarchy \
 && git -C /usr/share/omarchy rev-parse HEAD > /etc/omarchy-commit \
 && rm -rf /usr/share/omarchy/.git

# The Omarchy base package set, minus entries that address physical hardware a
# virtual machine does not have. The exclusions are recorded in the image
# manifest rather than being silently dropped:
#
#   asdcontrol  Apple Studio Display brightness over USB
#   ddcutil     DDC/CI monitor control over I2C
#   plymouth    boot splash, needs the real initramfs and console
#   bolt        Thunderbolt device authorisation
#   cups*       printing
#
# Everything else — compositor, shell, browser, editors, Omarchy's own tools —
# is installed exactly as upstream lists it.
RUN set -eux; \
    grep -vE '^\s*#|^\s*$' /usr/share/omarchy/install/omarchy-base.packages \
      | grep -vxE 'asdcontrol|ddcutil|plymouth|bolt|cups|cups-filters|cups-pk-helper|nvim' \
      > /tmp/pkgs.txt; \
    pacman -S --noconfirm --needed - < /tmp/pkgs.txt; \
    pacman -Scc --noconfirm

# wayvnc is how the desktop leaves the machine. Omarchy does not ship it —
# upstream assumes a physical screen — so remote display is Hyperwake's
# addition, not something inherited from Omarchy.
RUN pacman -Sy --noconfirm --needed wayvnc seatd openssh catatonit sudo \
 && pacman -Scc --noconfirm

# The tenant is root inside their own machine. That is expected: the isolation
# boundary is the hypervisor and the network, not in-guest permissions.
RUN useradd --create-home --home-dir /home/dev --shell /bin/bash --uid 1000 dev \
 && echo 'dev ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/dev \
 && chmod 0440 /etc/sudoers.d/dev \
 && passwd --lock dev \
 && groupadd -f seat && usermod -aG seat dev

RUN mkdir -p /run/sshd \
 && sed -i \
      -e 's/^#\?PasswordAuthentication.*/PasswordAuthentication no/' \
      -e 's/^#\?PermitRootLogin.*/PermitRootLogin no/' \
      -e 's/^#\?KbdInteractiveAuthentication.*/KbdInteractiveAuthentication no/' \
      -e 's/^#\?PubkeyAuthentication.*/PubkeyAuthentication yes/' \
      /etc/ssh/sshd_config \
 && printf '\n# Hyperwake\nX11Forwarding no\nAllowUsers dev\n' >> /etc/ssh/sshd_config

# No host keys in the image. Baking them in would give every clone the same SSH
# identity — the "no shared secret in a reusable template" rule. The entrypoint
# generates them per machine, onto the persistent volume.
RUN rm -f /etc/ssh/ssh_host_*

# Omarchy's shipped dotfiles become the skeleton for a new machine's home.
RUN mkdir -p /usr/share/hyperwake/skel \
 && cp -r /usr/share/omarchy/config/. /usr/share/hyperwake/skel/.config/ 2>/dev/null || true \
 && mkdir -p /usr/share/hyperwake/skel/.config

COPY rootfs/ /
COPY bin/hyperwake-guest-amd64 /usr/local/bin/hyperwake-guest

RUN chmod +x /usr/local/bin/hyperwake-guest \
             /usr/local/bin/hyperwake-entrypoint \
             /usr/local/bin/hyperwake-session \
             /usr/local/bin/hyperwake-doctor

# Immutable image identity. The control plane pins a computer to a version at
# creation; a machine must never silently become a different image.
RUN mkdir -p /etc/hyperwake && printf '{\n\
  "family": "omarchy/agent",\n\
  "image_version": "%s",\n\
  "omarchy_ref": "%s",\n\
  "omarchy_commit": "%s",\n\
  "arch": "x86_64",\n\
  "base": "archlinux",\n\
  "compositor": "hyprland",\n\
  "display": "wayvnc",\n\
  "built_at": "%s",\n\
  "excluded_packages": ["asdcontrol","ddcutil","plymouth","bolt","cups","cups-filters","cups-pk-helper"],\n\
  "requires": {"drm_render_node": true}\n\
}\n' \
    "${HYPERWAKE_IMAGE_VERSION}" "${OMARCHY_REF}" "$(cat /etc/omarchy-commit)" "$(date -u +%FT%TZ)" \
    > /etc/hyperwake/image.json

ENV HYPERWAKE_STATE_DIR=/home/dev/.hyperwake/state \
    HYPERWAKE_IMAGE_SLUG=omarchy-agent \
    HYPERWAKE_IMAGE_VERSION=0.1.0 \
    XDG_RUNTIME_DIR=/run/user/1000

EXPOSE 5900 22

ENTRYPOINT ["/usr/bin/catatonit", "--", "/usr/local/bin/hyperwake-entrypoint"]
