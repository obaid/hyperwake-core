#!/bin/bash
# Works only on the disposable copy mounted at /work, never the source factory.
set -euo pipefail
mkdir -p /guest
trap 'umount -R /guest 2>/dev/null || true' EXIT
e2fsck -fy /work/root.ext4 || test "$?" -eq 1
resize2fs /work/root.ext4
mount -o loop /work/root.ext4 /guest
mount -t proc proc /guest/proc
mount --rbind /dev /guest/dev
mount --make-rslave /guest/dev
mkdir -p /guest/run
mount -t tmpfs tmpfs /guest/run
cp -L /etc/resolv.conf /guest/run/hyperwake-resolv.conf
rm -f /guest/etc/resolv.conf
cp /guest/run/hyperwake-resolv.conf /guest/etc/resolv.conf
# Keep the factory's Hyprland/aquamarine pins. Only add the remote display tool.
chroot /guest /bin/bash -c 'pacman -Sy --noconfirm --needed wayvnc; id dev >/dev/null 2>&1 || useradd -m -u 1000 -s /bin/bash dev; groupadd -f seat; usermod -aG seat,video,render dev; passwd -l dev; mkdir -p /etc/sudoers.d; echo "dev ALL=(ALL) NOPASSWD: ALL" > /etc/sudoers.d/hyperwake; chmod 440 /etc/sudoers.d/hyperwake'
# Use the working wlr screencopy path with the pinned ARM compositor,
# bypassing its incompatible power and ext-image-copy negotiation. Keep the exact source and the small opt-in compatibility patch.
mkdir -p /guest/usr/local/src/hyperwake
curl -fsSL https://codeload.github.com/any1/wayvnc/tar.gz/ae53f076a83ad1ecd0d2adcaa063674a632bfe0f -o /guest/usr/local/src/hyperwake/wayvnc.tar.gz
echo 'bd6f94db2b8ddb63b74738186b3553f3f0c8ac8ea1295fdfbb35d1626e1e78d1  /guest/usr/local/src/hyperwake/wayvnc.tar.gz' | sha256sum -c -
tar -xf /guest/usr/local/src/hyperwake/wayvnc.tar.gz -C /guest/usr/local/src/hyperwake
wayvnc_source=/guest/usr/local/src/hyperwake/wayvnc-ae53f076a83ad1ecd0d2adcaa063674a632bfe0f
cp "$wayvnc_source/src/wayland.c" "$wayvnc_source/src/wayland.c.upstream"
sed -i -e 's/if (CHECK_BIND(zwlr_output_power_manager_v1, 1))/if (!getenv("HYPERWAKE_WAYVNC_LEGACY_CAPTURE") \&\& CHECK_BIND(zwlr_output_power_manager_v1, 1))/' -e 's/if (CHECK_BIND(ext_image_copy_capture_manager_v1, MIN(1, version)))/if (!getenv("HYPERWAKE_WAYVNC_LEGACY_CAPTURE") \&\& CHECK_BIND(ext_image_copy_capture_manager_v1, MIN(1, version)))/' -e 's/if (CHECK_BIND(ext_data_control_manager_v1, 1))/if (!getenv("HYPERWAKE_WAYVNC_DISABLE_CLIPBOARD") \&\& CHECK_BIND(ext_data_control_manager_v1, 1))/' -e 's/if (CHECK_BIND(zwlr_data_control_manager_v1, 2))/if (!getenv("HYPERWAKE_WAYVNC_DISABLE_CLIPBOARD") \&\& CHECK_BIND(zwlr_data_control_manager_v1, 2))/' "$wayvnc_source/src/wayland.c"
test "$(grep -c HYPERWAKE_WAYVNC_LEGACY_CAPTURE "$wayvnc_source/src/wayland.c")" -eq 2
test "$(grep -c HYPERWAKE_WAYVNC_DISABLE_CLIPBOARD "$wayvnc_source/src/wayland.c")" -eq 2
chroot /guest /bin/bash -c 'pacman -S --noconfirm --needed gcc make meson ninja pkgconf wayland-protocols; cd /usr/local/src/hyperwake/wayvnc-ae53f076a83ad1ecd0d2adcaa063674a632bfe0f; meson setup --wipe build --prefix=/usr/local -Dman-pages=disabled -Dtests=false; ninja -C build; install -m 755 build/wayvnc /usr/local/bin/wayvnc; pacman -Scc --noconfirm'
printf 'export HYPERWAKE_WAYVNC_LEGACY_CAPTURE=1\nexport HYPERWAKE_WAYVNC_DISABLE_CLIPBOARD=1\n' > /guest/etc/hyperwake-session.env
cp /overlay/hyperwake-guest /guest/usr/local/bin/hyperwake-guest
cp /repo/image/omarchy/rootfs/usr/local/bin/hyperwake-{entrypoint,session} /guest/usr/local/bin/
cp /repo/image/kvm/guest-start /guest/usr/local/bin/hyperwake-vm-start
cp /repo/image/kvm/guest.service /guest/etc/systemd/system/hyperwake.service
chmod +x /guest/usr/local/bin/hyperwake-*
mkdir -p /guest/usr/share/hyperwake/skel/.config /guest/usr/share/hyperwake/skel/.local/share /guest/etc/hyperwake
omarchy=/guest/usr/share/omarchy
if [ ! -d "$omarchy/config" ]; then omarchy=/guest/etc/skel/.local/share/omarchy; fi
test -d "$omarchy/config"
cp -a "$omarchy/config/." /guest/usr/share/hyperwake/skel/.config/
# Use an unscaled remote desktop; this is only copied on first boot.
sed -i -e 's/local omarchy_gdk_scale = 2/local omarchy_gdk_scale = 1/' -e 's/local omarchy_monitor_scale = "auto"/local omarchy_monitor_scale = 1/' /guest/usr/share/hyperwake/skel/.config/hypr/monitors.lua
ln -sfn "${omarchy#/guest}" /guest/usr/share/hyperwake/skel/.local/share/omarchy
printf '{"family":"omarchy/agent","arch":"aarch64","base":"try-omarchy","compositor":"hyprland"}\n' > /guest/etc/hyperwake/image.json
mkdir -p /guest/etc/systemd/system/multi-user.target.wants /guest/etc/systemd/network
ln -sfn /etc/systemd/system/hyperwake.service /guest/etc/systemd/system/multi-user.target.wants/hyperwake.service
ln -sfn /usr/lib/systemd/system/systemd-networkd.service /guest/etc/systemd/system/multi-user.target.wants/systemd-networkd.service
ln -sfn /usr/lib/systemd/system/systemd-resolved.service /guest/etc/systemd/system/multi-user.target.wants/systemd-resolved.service
printf '[Match]\nName=en* eth*\n[Network]\nDHCP=yes\n' > /guest/etc/systemd/network/20-hyperwake.network
ln -sfn /dev/null /guest/etc/systemd/system/sshd.service
ln -sfn /dev/null /guest/etc/systemd/system/omarchy-provision-owner.service
printf '\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nPermitRootLogin no\n' >> /guest/etc/ssh/sshd_config
rm -f /guest/etc/machine-id /guest/etc/ssh/ssh_host_*
touch /guest/etc/machine-id
sync
