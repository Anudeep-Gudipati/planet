# Quick Start
# -----------
# make production:
#     CD/CD build of Planet. This is what's used by Jenkins builds and this
#     is what gets released to customers.
#
# make:
#     builds your changes and updates planet binary in
#     build/current/rootfs/usr/bin/planet
#
# make start:
#     starts Planet from build/dev/rootfs/usr/bin/planet
#
# Build Steps
# -----------
# The sequence of steps the build process takes:
#     1. Make 'os' Docker image: the empty Debian 8 image.
#     2. Make 'base' image on top of 'os' (Debian + our additions)
#     3. Make 'buildbox' image on top of 'os'. Used for building,
#        not part of the Planet image.
#     4. Build various components (flannel, etcd, k8s, etc) inside
#        of the 'buildbox' based on inputs (master/node/dev)
#     5. Store everything inside a temporary Docker image based on 'base'
#     6. Export the root FS of that image into build/current/rootfs
#     7. build/current/rootfs is basically the output of the build.
#     8. Last, rootfs is stored into a ready for distribution tarball.
#
.DEFAULT_GOAL:=all
SHELL:=/bin/bash

PWD := $(shell pwd)
ASSETS := $(PWD)/build.assets
BUILD_ASSETS := $(PWD)/build/assets
BUILDDIR ?= $(PWD)/build
BUILDDIR := $(shell realpath $(BUILDDIR))

KUBE_VER := v1.9.6
SECCOMP_VER :=  2.3.1-2.1
DOCKER_VER := 17.03.2
# we currently use our own flannel fork: gravitational/flannel
FLANNEL_VER := master
ETCD_VER := v2.3.8
HELM_VER := v2.8.1
BUILDBOX_GO_VER := 1.9

PUBLIC_IP := 127.0.0.1
export
PLANET_PACKAGE_PATH=$(PWD)
PLANET_PACKAGE=github.com/gravitational/planet
PLANET_VERSION_PACKAGE_PATH=$(PLANET_PACKAGE)/Godeps/_workspace/src/github.com/gravitational/version

.PHONY: all
all: production

.PHONY: build
# 'make build' compiles the Go portion of Planet, meant for quick & iterative
# development on an _already built image_. You need to build an image first, for
# example with "make dev"
build: $(BUILDDIR)/current
	GOOS=linux GOARCH=amd64 go install -ldflags "$(PLANET_GO_LDFLAGS)" github.com/gravitational/planet/tool/planet
	cp -f $$GOPATH/bin/planet $(BUILDDIR)/current/planet
	rm -f $(BUILDDIR)/current/rootfs/usr/bin/planet
	cp -f $$GOPATH/bin/planet $(BUILDDIR)/current/rootfs/usr/bin/planet

.PHONY: deploy
# Deploys the build artifacts to Amazon S3
deploy:
	$(MAKE) -C $(ASSETS)/makefiles/deploy

.PHONY: production
production: buildbox
	@rm -f $(BUILD_ASSETS)/planet
	$(MAKE) -C $(ASSETS)/makefiles -f buildbox.mk

.PHONY: enter-buildbox
enter-buildbox:
	$(MAKE) -C $(ASSETS)/makefiles -e -f buildbox.mk enter-buildbox


.PHONY: test
# Run package tests
test: remove-temp-files
	go test -race -v -test.parallel=0 ./lib/... ./tool/...

.PHONY: test-package
# Test a specific package
test-package: remove-temp-files
	go test -race -v -test.parallel=0 ./$(p)

.PHONY: test-package-with-etcd
test-package-with-etcd: remove-temp-files
	PLANET_TEST_ETCD_NODES=http://127.0.0.1:4001 go test -v -test.parallel=0 ./$(p)

.PHONY: remove-temp-files
remove-temp-files:
	find . -name flymake_* -delete

.PHONY: start
# Start the planet container locally
start: build prepare-to-run
	cd $(BUILDDIR)/current && sudo rootfs/usr/bin/planet start \
		--debug \
		--etcd-member-name=local-planet \
		--secrets-dir=/var/planet/state \
		--public-ip=$(PUBLIC_IP) \
		--role=master \
		--service-uid=1000 \
		--initial-cluster=local-planet:$(PUBLIC_IP) \
		--volume=/var/planet/agent:/ext/agent \
		--volume=/var/planet/etcd:/ext/etcd \
		--volume=/var/planet/registry:/ext/registry \
		--volume=/var/planet/docker:/ext/docker

# Stop the running planet container
.PHONY: stop
stop:
	cd $(BUILDDIR)/current && sudo rootfs/usr/bin/planet --debug stop

# Enter the running planet container
.PHONY: enter
enter:
	cd $(BUILDDIR)/current && sudo rootfs/usr/bin/planet enter --debug /bin/bash

.PHONY: os
# Build the base Docker image everything else is based on.
os:
	@echo -e "\n---> Making Planet/OS (Debian) Docker image...\n"
	$(MAKE) -e BUILDIMAGE=planet/os DOCKERFILE=os.dockerfile make-docker-image

.PHONY: base
# Build the image with components required for running a Kubernetes node
base: os
	@echo -e "\n---> Making Planet/Base Docker image based on Planet/OS...\n"
	$(MAKE) -e BUILDIMAGE=planet/base DOCKERFILE=base.dockerfile \
		EXTRA_ARGS="--build-arg SECCOMP_VER=$(SECCOMP_VER) --build-arg DOCKER_VER=$(DOCKER_VER) --build-arg HELM_VER=$(HELM_VER)" \
		make-docker-image

.PHONY: buildbox
# Build a container used for building the planet image
buildbox: base
	@echo -e "\n---> Making Planet/BuildBox Docker image:\n" ;\
	$(MAKE) -e BUILDIMAGE=planet/buildbox \
		DOCKERFILE=buildbox.dockerfile \
		EXTRA_ARGS="--build-arg GOVERSION=$(BUILDBOX_GO_VER)" \
		make-docker-image

.PHONY: clean
# Remove build aftifacts
clean:
	$(MAKE) -C $(ASSETS)/makefiles -f buildbox.mk clean
	rm -rf $(BUILDDIR)

.PHONY: make-docker-image
# internal use:
make-docker-image:
	cd $(ASSETS)/docker; docker build $(EXTRA_ARGS) -t $(BUILDIMAGE) -f $(DOCKERFILE) .

.PHONY: remove-godeps
remove-godeps:
	rm -rf Godeps/
	find . -iregex .*go | xargs sed -i 's:".*Godeps/_workspace/src/:":g'

.PHONY: prepare-to-run
prepare-to-run: build
	@sudo mkdir -p /var/planet/registry /var/planet/etcd /var/planet/docker
	@sudo chown $$USER:$$USER /var/planet/etcd -R
	@cp -f $(BUILDDIR)/current/planet $(BUILDDIR)/current/rootfs/usr/bin/planet

.PHONY: clean-containers
clean-containers:
	@echo -e "\n---> Removing dead Docker/planet containers...\n"
	DEADCONTAINTERS=$$(docker ps --all | grep "planet" | awk '{print $$1}') ;\
	if [ ! -z "$$DEADCONTAINTERS" ] ; then \
		docker rm -f $$DEADCONTAINTERS ;\
	fi

.PHONY: clean-images
clean-images: clean-containers
	@echo -e "\n---> Removing old Docker/planet images...\n"
	DEADIMAGES=$$(docker images | grep "planet/" | awk '{print $$3}') ;\
	if [ ! -z "$$DEADIMAGES" ] ; then \
		docker rmi -f $$DEADIMAGES ;\
	fi

$(BUILDDIR)/current:
	@echo "You need to build the full image first. Run \"make dev\""

.PHONY: fix-logrus
fix-logrus:
	find vendor -type f -print0 | xargs -0 sed -i 's/Sirupsen/sirupsen/g'
