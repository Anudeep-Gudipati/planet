.PHONY: all

BINDIR:=$(ASSETDIR)/k8s-$(KUBE_VER)

all: k8s-master.mk
	@echo "\n---> Building master k8s components\n"
	cp -af ./kube-apiserver.service $(ROOTFS)/lib/systemd/system
	ln -sf /lib/systemd/system/kube-apiserver.service  $(ROOTFS)/lib/systemd/system/multi-user.target.wants/
	cp -af ./kube-controller-manager.service $(ROOTFS)/lib/systemd/system
	ln -sf /lib/systemd/system/kube-controller-manager.service  $(ROOTFS)/lib/systemd/system/multi-user.target.wants/
	cp -af ./kube-scheduler.service $(ROOTFS)/lib/systemd/system
	ln -sf /lib/systemd/system/kube-scheduler.service  $(ROOTFS)/lib/systemd/system/multi-user.target.wants/
	cp -af ./kube-kubelet.service $(ROOTFS)/lib/systemd/system
	ln -sf /lib/systemd/system/kube-kubelet.service  $(ROOTFS)/lib/systemd/system/multi-user.target.wants/
	cp -af ./kube-proxy.service $(ROOTFS)/lib/systemd/system
	ln -sf /lib/systemd/system/kube-proxy.service  $(ROOTFS)/lib/systemd/system/multi-user.target.wants/
	cp -af ./kube-dns.service $(ROOTFS)/lib/systemd/system
	ln -sf /lib/systemd/system/kube-dns.service  $(ROOTFS)/lib/systemd/system/multi-user.target.wants/
	install -m 0755 $(BINDIR)/kube-apiserver $(ROOTFS)/usr/bin
	install -m 0755 $(BINDIR)/kube-controller-manager $(ROOTFS)/usr/bin
	install -m 0755 $(BINDIR)/kube-scheduler $(ROOTFS)/usr/bin
	install -m 0755 $(BINDIR)/kubectl $(ROOTFS)/usr/bin
	install -m 0755 $(BINDIR)/kube-proxy $(ROOTFS)/usr/bin
	install -m 0755 $(BINDIR)/kubelet $(ROOTFS)/usr/bin
	install -m 0755 $(BINDIR)/kube-dns $(ROOTFS)/usr/bin
