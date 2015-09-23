.PHONY: all

all: 
	@echo "\n---> Building Kubernets-node components (kubelet, kube-proxy)\n"
	cp -af ./kube-kubelet.service $(ROOTFS)/lib/systemd/system
	ln -sf /lib/systemd/system/kube-kubelet.service  $(ROOTFS)/lib/systemd/system/multi-user.target.wants/
	cp -af ./kube-proxy.service $(ROOTFS)/lib/systemd/system
	ln -sf /lib/systemd/system/kube-proxy.service  $(ROOTFS)/lib/systemd/system/multi-user.target.wants/
	install -m 0755 $(TARGETDIR)/kube-proxy $(ROOTFS)/usr/bin
	install -m 0755 $(TARGETDIR)/kubelet $(ROOTFS)/usr/bin
