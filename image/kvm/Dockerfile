# A real Linux VM in a Docker-managed process. Build the pinned Omarchy base first.
ARG OMARCHY_IMAGE=hyperwake/omarchy:dev
FROM ${OMARCHY_IMAGE} AS guest
USER root
RUN pacman -Syu --noconfirm --needed linux mkinitcpio python systemd-sysvcompat \
    && printf 'MODULES=(virtio_pci virtio_blk virtio_gpu ext4)\nBINARIES=()\nFILES=()\nHOOKS=(base udev modconf block filesystems)\n' > /etc/mkinitcpio.conf \
    && kernel=$(ls /usr/lib/modules | head -1) \
    && mkdir -p /boot \
    && cp /usr/lib/modules/$kernel/vmlinuz /boot/vmlinuz-linux \
    && mkinitcpio -k "$kernel" -g /boot/initramfs-linux.img \
    && pacman -Scc --noconfirm
COPY guest-start /usr/local/bin/hyperwake-vm-start
COPY guest.service /etc/systemd/system/hyperwake.service
RUN chmod +x /usr/local/bin/hyperwake-vm-start \
    && mkdir -p /etc/systemd/system/multi-user.target.wants /etc/systemd/network \
    && ln -sf /etc/systemd/system/hyperwake.service /etc/systemd/system/multi-user.target.wants/hyperwake.service \
    && ln -sf /usr/lib/systemd/system/systemd-networkd.service /etc/systemd/system/multi-user.target.wants/systemd-networkd.service \
    && ln -sf /usr/lib/systemd/system/systemd-resolved.service /etc/systemd/system/multi-user.target.wants/systemd-resolved.service \
    && printf '[Match]\nName=en* eth*\n[Network]\nDHCP=yes\n' > /etc/systemd/network/20-hyperwake.network \
    && rm -f /etc/machine-id /etc/ssh/ssh_host_* \
    && touch /etc/machine-id \
    && mkdir -p /usr/share/hyperwake/skel/.local/share \
    && ln -sfn /usr/share/omarchy /usr/share/hyperwake/skel/.local/share/omarchy

# Filesystem assembly is architecture-independent; keep it native even when
# cross-building the x86_64 guest image on an ARM development machine.
FROM --platform=$BUILDPLATFORM debian:bookworm-slim AS disk
RUN apt-get update && apt-get install -y --no-install-recommends e2fsprogs zstd && rm -rf /var/lib/apt/lists/*
RUN --mount=type=bind,from=guest,source=/,target=/rootfs,ro \
    mkdir -p /output \
    && cp /rootfs/boot/vmlinuz-linux /rootfs/boot/initramfs-linux.img /output/ \
    && truncate -s 16G /tmp/root.ext4 \
    && mkfs.ext4 -F -d /rootfs /tmp/root.ext4 \
    && zstd -T2 -3 /tmp/root.ext4 -o /output/root.ext4.zst \
    && rm /tmp/root.ext4

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    qemu-system-x86 qemu-system-gui qemu-utils xvfb xauth libgl1-mesa-dri mesa-utils \
    python3 e2fsprogs zstd tini \
    && rm -rf /var/lib/apt/lists/*
COPY --from=disk /output/ /opt/hyperwake/
COPY launch.py /usr/local/bin/hyperwake-vm
ENV LIBGL_ALWAYS_SOFTWARE=1
VOLUME /data
EXPOSE 22 5900
ENTRYPOINT ["/usr/bin/tini", "--", "python3", "/usr/local/bin/hyperwake-vm"]
