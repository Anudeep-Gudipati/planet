FROM ubuntu:15.04

RUN sed -i 's/archive.ubuntu.com/mirror.pnl.gov/g' /etc/apt/sources.list
RUN apt-get update
RUN apt-get install -y curl make git libc6-dev gcc tar gzip
RUN mkdir -p /opt && cd /opt && curl https://storage.googleapis.com/golang/go1.4.2.linux-amd64.tar.gz | tar xz

ENV GOPATH /gopath
ENV GOROOT /opt/go
ENV PATH $PATH:$GOPATH/bin:$GOROOT/bin

RUN mkdir -p $GOPATH/src $GOPATH/bin
ADD . $GOPATH/src/github.com/gravitational/cube

RUN go get github.com/klizhentas/deb2aci github.com/appc/spec/actool github.com/kr/godep
RUN go install github.com/klizhentas/deb2aci github.com/appc/spec/actool github.com/gravitational/cube/cube

ENV BUILDDIR /build
RUN mkdir -p $BUILDDIR/aci

ADD ./makefiles/ $BUILDDIR/makefiles

RUN ROOTFS=${BUILDDIR}/aci/rootfs make -C $BUILDDIR/makefiles/cube-base/image -f image.mk
RUN ROOTFS=${BUILDDIR}/aci/rootfs make -C $BUILDDIR/makefiles/cube-base/network -f network.mk
RUN ROOTFS=${BUILDDIR}/aci/rootfs make -C $BUILDDIR/makefiles/cube-base/docker -f docker.mk 
RUN ROOTFS=${BUILDDIR}/aci/rootfs make -C $BUILDDIR/makefiles/cube-registry -f registry.mk

RUN mkdir -p ${BUILDDIR}/aci/rootfs/usr/bin && cp $GOPATH/bin/cube ${BUILDDIR}/aci/rootfs/usr/bin && chown 755 ${BUILDDIR}/aci/rootfs/usr/bin/cube
