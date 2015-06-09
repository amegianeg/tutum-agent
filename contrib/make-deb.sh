#!/bin/bash

DEST=$1

VERSION=$(cat VERSION)
PKGVERSION="${VERSION:-latest}"
PACKAGE_ARCHITECTURE="${ARCHITECTURE:-amd64}"
PACKAGE_URL="http://www.tutum.co/"
PACKAGE_MAINTAINER="support@tutum.co"
PACKAGE_DESCRIPTION="Agent to manage Docker hosts through Tutum"
PACKAGE_LICENSE="Proprietary"

bundle_ubuntu() {
  DIR=$DEST/staging

  # Include our init scripts
  mkdir -p $DIR/etc/init $DIR/etc/init.d $DIR/lib/systemd/system/
  cp contrib/upstart/tutum-agent.conf $DIR/etc/init/
  cp contrib/init.d/tutum-agent $DIR/etc/init.d/
  cp contrib/systemd/tutum-agent.socket $DIR/lib/systemd/system/
  cp contrib/systemd/tutum-agent.service $DIR/lib/systemd/system/

  # Copy the binary
  # This will fail if the binary bundle hasn't been built
  mkdir -p $DIR/usr/bin
  cp /build/bin/linux/$PACKAGE_ARCHITECTURE/tutum-agent-$PKGVERSION $DIR/usr/bin/tutum-agent

  cat > $DEST/postinst <<EOF
#!/bin/sh
set -e

DOCKER_UPSTART_CONF="/etc/init/docker.conf"
if [ -f "${DOCKER_UPSTART_CONF}" ]; then
  echo "Removing conflicting docker upstart configuration file at ${DOCKER_UPSTART_CONF}..."
  rm -f ${DOCKER_UPSTART_CONF}
fi

if ! getent group docker > /dev/null; then
  groupadd --system docker
fi

if [ -n "$2" ]; then
  _dh_action=restart
else
  _dh_action=start
fi
service tutum-agent $_dh_action 2>/dev/null || true

#DEBHELPER#
EOF

  cat > $DEST/prerm <<EOF
#!/bin/sh
set -e

case "$1" in
  remove)
    service tutum-agent stop 2>/dev/null || true
  ;;
esac

#DEBHELPER#
EOF

  cat > $DEST/postrm <<EOF
#!/bin/sh
set -e

case "$1" in
  remove)
    rm -fr /usr/bin/docker /usr/lib/tutum
  ;;
  purge)
    rm -fr /usr/bin/docker /usr/lib/tutum /etc/tutum
  ;;
esac

# In case this system is running systemd, we make systemd reload the unit files
# to pick up changes.
if [ -d /run/systemd/system ] ; then
  systemctl --system daemon-reload > /dev/null || true
fi

#DEBHELPER#
EOF

  chmod +x $DEST/postinst $DEST/prerm $DEST/postrm

  (
    # switch directories so we create *.deb in the right folder
    cd $DEST

    # create tutum-agent-$PKGVERSION package
    fpm -s dir -C $DIR \
      --name tutum-agent --version $PKGVERSION \
      --after-install $DEST/postinst \
      --before-remove $DEST/prerm \
      --after-remove $DEST/postrm \
      --architecture "$PACKAGE_ARCHITECTURE" \
      --prefix / \
      --description "$PACKAGE_DESCRIPTION" \
      --maintainer "$PACKAGE_MAINTAINER" \
      --conflicts docker \
      --conflicts docker.io \
      --conflicts lxc-docker \
      --deb-recommends "cgroup-lite | cgroupfs-mount" \
      --depends aufs-tools \
      --depends iptables \
      --depends "libapparmor1 >= 2.6~devel" \
      --depends "libc6 >= 2.4" \
      --depends "libdevmapper1.02.1 >= 2:1.02.63" \
      --depends "libsqlite3-0 >= 3.5.9" \
      --depends perl \
      --depends gnupg \
      --depends "sysv-rc >= 2.88dsf-24" \
      --depends xz-utils \
      --provides tutum-agent \
      --replaces tutum-agent \
      --url "$PACKAGE_URL" \
      --license "$PACKAGE_LICENSE" \
      --config-files "etc/init/tutum-agent.conf" \
      --config-files "etc/init.d/tutum-agent" \
      --config-files "lib/systemd/system/tutum-agent.socket" \
      --config-files "lib/systemd/system/tutum-agent.service" \
      --deb-compression gz \
      -t deb .
  )

  rm $DEST/postinst $DEST/prerm $DEST/postrm
  rm -r $DIR
}

bundle_ubuntu