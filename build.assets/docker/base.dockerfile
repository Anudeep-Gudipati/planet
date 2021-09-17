ARG PLANET_OS_IMAGE=planet/os
FROM $PLANET_OS_IMAGE

ARG SECCOMP_VER
ARG IPTABLES_VER
ARG DOCKER_VER
ARG HELM_VER
ARG COREDNS_VER

# FIXME: allowing downgrades and pinning the version of libip4tc0 for iptables
# as the package has a dependency on the older version as the one available.
RUN apt-get update && apt-get install -q -y --allow-downgrades bridge-utils \
        seccomp=$SECCOMP_VER \
        bash-completion \
        kmod \
        libip4tc0=1.6.0+snapshot20161117-6 \
        ebtables \
        libdevmapper1.02.1 \
        libsqlite3-0 \
        e2fsprogs \
        libncurses5 \
        net-tools \
        curl \
        iproute2 \
        lsb-base \
        dash \
        ca-certificates \
        aufs-tools \
        xfsprogs \
        dbus \
        dnsutils \
        ethtool \
        sysstat \
        nano \
        vim \
        iotop \
        htop \
        ifstat \
        iftop \
        traceroute \
        tcpdump \
        procps \
        coreutils \
        lsof \
        socat \
        nmap \
        netcat \
        nfs-common \
        jq \
        conntrack \
        open-iscsi \
        strace ; \
    apt-get -t testing install -y lvm2; \
    apt-get -y autoclean; apt-get -y clean

# We need to use a newer version of iptables than debian has available
# not ideal, but it's easier to run `make install` if we run this inline instead of a multi-stage build
RUN export DEBIAN_FRONTEND=noninteractive && set -ex \
        && apt-get update \
        && apt-get install -q -y --allow-downgrades --no-install-recommends \
        git \
        autoconf \
        libtool \
        automake \
        pkg-config \
        libmnl-dev \
        make \
        && mkdir /tmp/iptables.build \
        && git clone git://git.netfilter.org/iptables.git --branch ${IPTABLES_VER} --single-branch /tmp/iptables.build \
        && cd /tmp/iptables.build \
        && ./autogen.sh \
        && ./configure --disable-nftables \
        && make \
        && make install \
        && apt-get remove -y \
        git \
        autoconf \
        libtool \
        automake \
        pkg-config \
        libmnl-dev \
        make \
        && apt-get -y autoclean && apt-get -y clean && apt-get autoremove -y \
        && rm -rf /var/lib/apt/lists/*;

# do not install docker from Debian repositories but rather download static binaries for seccomp support
RUN curl https://download.docker.com/linux/static/stable/x86_64/docker-$DOCKER_VER.tgz -o /tmp/docker-$DOCKER_VER.tgz && \
    tar -xvzf /tmp/docker-$DOCKER_VER.tgz -C /tmp && \
    cp /tmp/docker/* /usr/bin && \
    rm -rf /tmp/docker*

# Replace containerd shipped with docker to avoid deadlocks / hangs due to missing exit result
# https://github.com/gravitational/gravity/issues/1842
# https://github.com/containerd/containerd/issues/3572#issuecomment-528293369
RUN curl -L https://github.com/containerd/containerd/releases/download/v1.2.10/containerd-1.2.10.linux-amd64.tar.gz -o /tmp/containerd.tgz && \
    mkdir -p /tmp/containerd && tar -xvzf /tmp/containerd.tgz -C /tmp/containerd && \
    cp /tmp/containerd/bin/containerd /tmp/containerd/bin/containerd-shim /tmp/containerd/bin/ctr /usr/bin/ && \
    rm -rf /tmp/containerd

RUN curl https://get.helm.sh/helm-$HELM_VER-linux-amd64.tar.gz -o /tmp/helm-$HELM_VER.tar.gz && \
    mkdir -p /tmp/helm && tar -xvzf /tmp/helm-$HELM_VER.tar.gz -C /tmp/helm && \
    cp /tmp/helm/linux-amd64/helm /usr/bin && \
    rm -rf /tmp/helm*

RUN curl -L https://github.com/coredns/coredns/releases/download/v${COREDNS_VER}/coredns_${COREDNS_VER}_linux_amd64.tgz -o /tmp/coredns-${COREDNS_VER}.tar.gz && \
    mkdir -p /tmp/coredns && tar -xvzf /tmp/coredns-${COREDNS_VER}.tar.gz -C /tmp/coredns && \
    cp /tmp/coredns/coredns /usr/bin && \
    rm -rf /tmp/coredns*

RUN groupadd --system --non-unique --gid 1000 planet ;\
    useradd --system --non-unique --no-create-home -g 1000 -u 1000 planet
