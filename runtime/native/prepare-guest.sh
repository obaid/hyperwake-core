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
cp -L /etc/resolv.conf /guest/run/mola-resolv.conf
rm -f /guest/etc/resolv.conf
cp /guest/run/mola-resolv.conf /guest/etc/resolv.conf
# Keep the factory's Hyprland/aquamarine pins. Only add the remote display tool.
chroot /guest /bin/bash -c 'pacman -Sy --noconfirm --needed wayvnc; id dev >/dev/null 2>&1 || useradd -m -u 1000 -s /bin/bash dev; groupadd -f seat; usermod -aG seat,video,render dev; passwd -l dev; mkdir -p /etc/sudoers.d; echo "dev ALL=(ALL) NOPASSWD: ALL" > /etc/sudoers.d/mola; chmod 440 /etc/sudoers.d/mola'
# Use the working wlr screencopy path with the pinned ARM compositor,
# bypassing its incompatible power and ext-image-copy negotiation. Keep the exact source and the small opt-in compatibility patch.
mkdir -p /guest/usr/local/src/mola
curl -fsSL https://codeload.github.com/any1/wayvnc/tar.gz/ae53f076a83ad1ecd0d2adcaa063674a632bfe0f -o /guest/usr/local/src/mola/wayvnc.tar.gz
echo 'bd6f94db2b8ddb63b74738186b3553f3f0c8ac8ea1295fdfbb35d1626e1e78d1  /guest/usr/local/src/mola/wayvnc.tar.gz' | sha256sum -c -
tar -xf /guest/usr/local/src/mola/wayvnc.tar.gz -C /guest/usr/local/src/mola
wayvnc_source=/guest/usr/local/src/mola/wayvnc-ae53f076a83ad1ecd0d2adcaa063674a632bfe0f
cp "$wayvnc_source/src/wayland.c" "$wayvnc_source/src/wayland.c.upstream"
sed -i -e 's/if (CHECK_BIND(zwlr_output_power_manager_v1, 1))/if (!getenv("MOLA_WAYVNC_LEGACY_CAPTURE") \&\& CHECK_BIND(zwlr_output_power_manager_v1, 1))/' -e 's/if (CHECK_BIND(ext_image_copy_capture_manager_v1, MIN(1, version)))/if (!getenv("MOLA_WAYVNC_LEGACY_CAPTURE") \&\& CHECK_BIND(ext_image_copy_capture_manager_v1, MIN(1, version)))/' -e 's/if (CHECK_BIND(ext_data_control_manager_v1, 1))/if (!getenv("MOLA_WAYVNC_DISABLE_CLIPBOARD") \&\& CHECK_BIND(ext_data_control_manager_v1, 1))/' -e 's/if (CHECK_BIND(zwlr_data_control_manager_v1, 2))/if (!getenv("MOLA_WAYVNC_DISABLE_CLIPBOARD") \&\& CHECK_BIND(zwlr_data_control_manager_v1, 2))/' "$wayvnc_source/src/wayland.c"
test "$(grep -c MOLA_WAYVNC_LEGACY_CAPTURE "$wayvnc_source/src/wayland.c")" -eq 2
test "$(grep -c MOLA_WAYVNC_DISABLE_CLIPBOARD "$wayvnc_source/src/wayland.c")" -eq 2
chroot /guest /bin/bash -c 'pacman -S --noconfirm --needed gcc make meson ninja pkgconf wayland-protocols; cd /usr/local/src/mola/wayvnc-ae53f076a83ad1ecd0d2adcaa063674a632bfe0f; meson setup --wipe build --prefix=/usr/local -Dman-pages=disabled -Dtests=false; ninja -C build; install -m 755 build/wayvnc /usr/local/bin/wayvnc; pacman -Scc --noconfirm'
printf 'export MOLA_WAYVNC_LEGACY_CAPTURE=1\nexport MOLA_WAYVNC_DISABLE_CLIPBOARD=1\n' > /guest/etc/mola-session.env
cp /overlay/mola-guest /guest/usr/local/bin/mola-guest
cp /repo/image/omarchy/rootfs/usr/local/bin/mola-{entrypoint,session} /guest/usr/local/bin/
cp /repo/image/kvm/guest-start /guest/usr/local/bin/mola-vm-start
cp /repo/image/kvm/guest.service /guest/etc/systemd/system/mola.service
chmod +x /guest/usr/local/bin/mola-*
mkdir -p /guest/usr/share/mola/skel/.config /guest/usr/share/mola/skel/.local/share /guest/etc/mola
omarchy=/guest/usr/share/omarchy
if [ ! -d "$omarchy/config" ]; then omarchy=/guest/etc/skel/.local/share/omarchy; fi
test -d "$omarchy/config"
cp -a "$omarchy/config/." /guest/usr/share/mola/skel/.config/
# Use an unscaled remote desktop; this is only copied on first boot.
sed -i -e 's/local omarchy_gdk_scale = 2/local omarchy_gdk_scale = 1/' -e 's/local omarchy_monitor_scale = "auto"/local omarchy_monitor_scale = 1/' /guest/usr/share/mola/skel/.config/hypr/monitors.lua
ln -sfn "${omarchy#/guest}" /guest/usr/share/mola/skel/.local/share/omarchy

# A machine built from this image is a new Omarchy install, not an upgrade of
# an older one. omarchy-migrate cannot tell the difference: it lists every
# migration under $OMARCHY_PATH/migrations that has no marker in
# ~/.local/state/omarchy/migrations, so a brand-new machine reports every
# migration in the image as pending and the desktop keeps asking about changes
# it already has. Stamping them records what the image already contains, which
# is exactly what an in-place upgrade to this version would have left behind.
#
# Every home a machine can start from gets stamped, because which one applies
# depends on whether the factory image shipped a populated /home/dev.
#
# The same loop turns idling off. Omarchy covers the screen with a screensaver
# after 150 seconds without input, and input here means input: a command that
# runs for three minutes, or a model that thinks for three minutes, is idle as
# far as the compositor is concerned. The next screenshot then shows a
# screensaver instead of the desktop, which is a confusing thing to hand an
# agent that has no idea it went away. omarchy-toggle-idle records the choice
# as this file, so writing it is the supported way to ask for the same thing.
#
# The 300-second screen lock needs no help: it refuses to arm because dev has
# no password for PAM to check, and Omarchy declines to lock a session that
# could not then be unlocked.
migrations_stamped=0
for home in /guest/usr/share/mola/skel /guest/etc/skel /guest/home/dev; do
  [ -d "$home" ] || continue
  mkdir -p "$home/.local/state/omarchy/migrations"
  for migration in "$omarchy"/migrations/*.sh; do
    [ -f "$migration" ] || continue
    touch "$home/.local/state/omarchy/migrations/$(basename "$migration")"
    migrations_stamped=$((migrations_stamped + 1))
  done
  mkdir -p "$home/.local/state/omarchy/indicators"
  touch "$home/.local/state/omarchy/indicators/stay-awake"
done
chown -R 1000:1000 /guest/home/dev/.local 2>/dev/null || true
echo "Stamped $migrations_stamped Omarchy migration markers."
# Fail the build rather than shipping the nag again if upstream moves the
# directory. Silence here is how 86 pending migrations got baked in.
test "$migrations_stamped" -gt 0

# Omarchy onboards a human with notifications sent at -u critical, which the
# freedesktop spec says never expire. A person reads them and clicks them away.
# Nobody does that here, so they stay on screen for the life of the machine,
# stacked over the top-right corner of a 1280x800 screen, and every one of them
# carries an --exec that opens a fullscreen menu or a floating terminal on
# click. An agent that clicks near that corner gets a full-screen overlay it
# did not ask for and has no idea how to dismiss.
#
# Each of these five asks for something a disposable VM does not have: a
# wireless card, a fingerprint reader, a microphone, a favourite coding agent,
# or a reason to run a system update on a machine that is thrown away. The
# first-run scripts that actually set the desktop up (audio, user units, GTK
# settings, theme) are left alone.
#
# They are emptied rather than deleted. omarchy-provision-first-run names each
# one literally and treats a non-zero exit as a failed first run, which skips
# omarchy-done mark and so runs the whole sequence again at every login. A
# missing file exits 127, so removing these would trade a stuck notification
# for a first-run loop that never ends.
onboarding_silenced=0
for nag in welcome.sh wifi.sh setup-agent.hook install-voxtype.hook setup-fingerprint.hook; do
  [ -e "$omarchy/install/user/first-run/$nag" ] || continue
  printf '#!/bin/bash\n# Emptied by Mola: onboarding for a human who is not here.\nexit 0\n' \
    > "$omarchy/install/user/first-run/$nag"
  chmod 755 "$omarchy/install/user/first-run/$nag"
  onboarding_silenced=$((onboarding_silenced + 1))
done
echo "Silenced $onboarding_silenced Omarchy onboarding notifications."
# As with the migrations above: if upstream renames these, fail here rather
# than quietly shipping a desktop that wedges itself on the first stray click.
test "$onboarding_silenced" -eq 5

printf '{"family":"omarchy/agent","arch":"aarch64","base":"try-omarchy","compositor":"hyprland"}\n' > /guest/etc/mola/image.json
mkdir -p /guest/etc/systemd/system/multi-user.target.wants /guest/etc/systemd/network
ln -sfn /etc/systemd/system/mola.service /guest/etc/systemd/system/multi-user.target.wants/mola.service
ln -sfn /usr/lib/systemd/system/systemd-networkd.service /guest/etc/systemd/system/multi-user.target.wants/systemd-networkd.service
ln -sfn /usr/lib/systemd/system/systemd-resolved.service /guest/etc/systemd/system/multi-user.target.wants/systemd-resolved.service
printf '[Match]\nName=en* eth*\n[Network]\nDHCP=yes\n' > /guest/etc/systemd/network/20-mola.network
ln -sfn /dev/null /guest/etc/systemd/system/sshd.service
ln -sfn /dev/null /guest/etc/systemd/system/omarchy-provision-owner.service
printf '\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nPermitRootLogin no\n' >> /guest/etc/ssh/sshd_config
rm -f /guest/etc/machine-id /guest/etc/ssh/ssh_host_*
touch /guest/etc/machine-id
sync
