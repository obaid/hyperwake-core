# Hyperwake development desktop image.
#
# This is the LOCAL DEVELOPMENT stand-in for the production Omarchy image. It
# exists so the whole control plane — provisioning, desktop gateway, SSH,
# persistence, billing — can be exercised end to end on a laptop without a
# Proxmox host. It is NOT Omarchy, and the Image record that points at it is
# labelled `hyperwake-dev-desktop` for exactly that reason.
#
# Production builds the real thing with Packer from a pinned Omarchy ISO; see
# ../packer/. The contract this image must honour is the one the control plane
# depends on:
#
#   * a VNC display on :5900 inside the container
#   * sshd on :22, key auth only
#   * the guest daemon running and calling home
#   * /home/dev being the one directory that must survive stop/start
#
# Anything satisfying that contract is a valid Hyperwake image.

FROM debian:bookworm-slim

# noninteractive: there is no terminal during build, and debconf will otherwise
# block on tzdata.
ENV DEBIAN_FRONTEND=noninteractive \
    LANG=C.UTF-8 \
    DISPLAY=:1

# TigerVNC's Xtigervnc is both the X server and the VNC server in one process,
# which avoids the Xvfb + x11vnc pairing and its startup race.
RUN apt-get update && apt-get install -y --no-install-recommends \
        tigervnc-standalone-server \
        tigervnc-common \
        i3 \
        i3status \
        dunst \
        xterm \
        rxvt-unicode \
        fonts-jetbrains-mono \
        fonts-dejavu-core \
        xfonts-base \
        feh \
        imagemagick \
        x11-xserver-utils \
        x11-utils \
        xdg-utils \
        openssh-server \
        ca-certificates \
        curl \
        wget \
        git \
        vim \
        nano \
        less \
        htop \
        tmux \
        jq \
        ripgrep \
        unzip \
        procps \
        iproute2 \
        sudo \
        tini \
    && rm -rf /var/lib/apt/lists/*

# fastfetch is not in bookworm; fetch the release binary for this architecture.
# It is cosmetic — a failure here must not fail the image build.
ARG TARGETARCH
RUN set -eux; \
    case "${TARGETARCH:-$(dpkg --print-architecture)}" in \
        arm64) FF_ARCH=aarch64 ;; \
        amd64) FF_ARCH=amd64 ;; \
        *)     FF_ARCH="" ;; \
    esac; \
    if [ -n "$FF_ARCH" ]; then \
        curl -fsSL -o /tmp/fastfetch.deb \
            "https://github.com/fastfetch-cli/fastfetch/releases/download/2.30.1/fastfetch-linux-${FF_ARCH}.deb" \
            && dpkg -i /tmp/fastfetch.deb \
            && rm -f /tmp/fastfetch.deb \
            || echo "fastfetch unavailable for ${FF_ARCH}, continuing without it"; \
    fi

# The tenant is root inside their own machine — that is expected and is not the
# security boundary. The boundary is the hypervisor (here, the container) and
# the network. See docs/founder-pack/03_ARCHITECTURE.md.
RUN useradd --create-home --home-dir /home/dev --shell /bin/bash --uid 1000 dev \
    && echo 'dev ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/dev \
    && chmod 0440 /etc/sudoers.d/dev \
    && passwd --lock dev

# sshd: keys only. Password auth is never the platform default (PRD §5).
RUN mkdir -p /run/sshd \
    && sed -i \
        -e 's/^#\?PasswordAuthentication.*/PasswordAuthentication no/' \
        -e 's/^#\?PermitRootLogin.*/PermitRootLogin no/' \
        -e 's/^#\?ChallengeResponseAuthentication.*/ChallengeResponseAuthentication no/' \
        -e 's/^#\?KbdInteractiveAuthentication.*/KbdInteractiveAuthentication no/' \
        -e 's/^#\?PubkeyAuthentication.*/PubkeyAuthentication yes/' \
        /etc/ssh/sshd_config \
    && printf '\n# Hyperwake\nUsePAM yes\nX11Forwarding no\nAllowUsers dev\n' >> /etc/ssh/sshd_config

# Host keys are deliberately NOT generated here. Baking them into the image
# would give every clone the same SSH identity — the exact "no shared secret in
# a reusable template" rule from AGENTS.md. The entrypoint generates them per
# machine on first boot instead.
RUN rm -f /etc/ssh/ssh_host_*

# The skeleton is what a brand-new machine's home directory starts as. It lives
# outside /home/dev because /home/dev is a mount point at runtime.
COPY skel/ /usr/share/hyperwake/skel/
COPY rootfs/ /
COPY bin/hyperwake-guest /usr/local/bin/hyperwake-guest

RUN chmod +x /usr/local/bin/hyperwake-guest \
             /usr/local/bin/hyperwake-entrypoint \
             /usr/local/bin/hyperwake-wallpaper \
    && /usr/local/bin/hyperwake-wallpaper /usr/share/hyperwake/wallpaper.png

ENV HYPERWAKE_STATE_DIR=/var/lib/hyperwake \
    HYPERWAKE_IMAGE_SLUG=hyperwake-dev-desktop \
    HYPERWAKE_IMAGE_VERSION=0.1.0

EXPOSE 5900 22

# tini reaps the zombies that a multi-process container otherwise accumulates.
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/hyperwake-entrypoint"]
