.PHONY: all

all: registry.mk
	cp -af ./registry.service $(ROOTFS)/lib/systemd/system
	ln -sf /lib/systemd/system/registry.service  $(ROOTFS)/lib/systemd/system/multi-user.target.wants/
