# Quick Start
# -----------
# make dev: 
#     builds 'development' image of Planet, stores output in build/dev and 
#     points build/current symlink to it. 
#
# make: 
#     builds your changes and updates planet binary in 
#     build/current/rootfs/usr/bin/planet
#
# make dev-start:
#     starts Planet from build/dev/rootfs/usr/bin/planet
#
# make test:
#     starts Planet in self-test mode
#     requires `make dev` and `make dev-start`
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
#     8. Later, root FS is tarballed and ready for distribution.
#
.DEFAULT_GOAL:=all
SHELL:=/bin/bash
.PHONY: build os base buildbox dev master node testbox test

PWD := $(shell pwd)
ASSETS := $(PWD)/build.assets
BUILDDIR ?= $(PWD)/build
BUILDDIR := $(shell realpath $(BUILDDIR))
PLANETVER:=0.02
KUBE_VER:=v1.1.4
PUBLIC_IP:=127.0.0.1
export
PLANET_PACKAGE_PATH=$(PWD)
PLANET_PACKAGE=github.com/gravitational/planet
PLANET_VERSION_PACKAGE_PATH=$(PLANET_PACKAGE)/Godeps/_workspace/src/github.com/gravitational/version
PLANET_GO_LDFLAGS="$(shell linkflags -pkg=$(PLANET_PACKAGE_PATH) -verpkg=$(PLANET_VERSION_PACKAGE_PATH) -compat)"

all: production dev

# 'make build' compiles the Go portion of Planet, meant for quick & iterative 
# development on an _already built image_. You need to build an image first, for 
# example with "make dev"
build: $(BUILDDIR)/current
	GOOS=linux GOARCH=amd64 go install -ldflags $(PLANET_GO_LDFLAGS) github.com/gravitational/planet/tool/planet
	cp -f $$GOPATH/bin/planet $(BUILDDIR)/current/planet 
	rm -f $(BUILDDIR)/current/rootfs/usr/bin/planet
	cp -f $$GOPATH/bin/planet $(BUILDDIR)/current/rootfs/usr/bin/planet

# Makes a "developer" image, with _all_ parts of Kubernetes installed
dev: buildbox
	$(MAKE) -C $(ASSETS)/makefiles -e TARGET=dev PLANET_GO_LDFLAGS=$(PLANET_GO_LDFLAGS) -f buildbox.mk

# Composite image target that creates master/node images
production: buildbox
	$(MAKE) -C $(ASSETS)/makefiles -e TARGET=master PLANET_GO_LDFLAGS=$(PLANET_GO_LDFLAGS) -f buildbox.mk
	$(MAKE) -C $(ASSETS)/makefiles -e TARGET=node PLANET_GO_LDFLAGS=$(PLANET_GO_LDFLAGS) -f buildbox.mk

# Runs end-to-end tests in the specific environment
test: buildbox testbox prepare-to-run
	$(MAKE) -C $(ASSETS)/makefiles -e TARGET=dev TEST_FOCUS=$(SPEC) -f test.mk

# Starts "planet-dev" build and executes a self-test
# make test SPEC="Networking\|Pods"
dev-test: dev prepare-to-run
	cd $(BUILDDIR)/current && sudo rootfs/usr/bin/planet start\
		--self-test\
		--test-spec=$(SPEC)\
		--debug\
		--public-ip=$(PUBLIC_IP)\
		--role=master\
		--role=node\
		--volume=/var/planet/etcd:/ext/etcd\
		--volume=/var/planet/registry:/ext/registry\
		--volume=/var/planet/docker:/ext/docker\
		-- -progress -trace -p -noisyPendings=false -failFast=true

# Starts "planet-dev" build.
dev-start: dev prepare-to-run
	cd $(BUILDDIR)/current && sudo rootfs/usr/bin/planet start\
		--debug\
		--public-ip=$(PUBLIC_IP)\
		--role=master\
		--role=node\
		--volume=/var/planet/etcd:/ext/etcd\
		--volume=/var/planet/registry:/ext/registry\
		--volume=/var/planet/docker:/ext/docker

# Starts "planet-node" image.
node-start: node prepare-to-run
	cd $(BUILDDIR)/current && sudo rootfs/usr/bin/planet start\
		--role=node\
		--public-ip=$(PUBLIC_IP)\
		--volume=/var/planet/etcd:/ext/etcd\
		--volume=/var/planet/registry:/ext/registry\
		--volume=/var/planet/docker:/ext/docker

# Starts "planet-master" image.
master-start: master prepare-to-run
	cd $(BUILDDIR)/current && sudo rootfs/usr/bin/planet start\
		--role=master\
		--public-ip=$(PUBLIC_IP)\
		--volume=/var/planet/etcd:/ext/etcd\
		--volume=/var/planet/registry:/ext/registry\
		--volume=/var/planet/docker:/ext/docker

stop:
	cd $(BUILDDIR)/current && sudo rootfs/usr/bin/planet --debug stop

enter:
	cd $(BUILDDIR)/current && sudo rootfs/usr/bin/planet enter --debug /bin/bash

# Builds the base Docker image (bare bones OS). Everything else is based on. 
# Debian stable + configured locales. 
os: 
	@echo -e "\n---> Making Planet/OS (Debian) Docker image...\n"
	$(MAKE) -e BUILDIMAGE=planet/os DOCKERFILE=os.dockerfile make-docker-image

# Builds on top of "bare OS" image by adding components that every Kubernetes/planet node
# needs (like bridge-utils or kmod)
base: os
	@echo -e "\n---> Making Planet/Base Docker image based on Planet/OS...\n"
	$(MAKE) -e BUILDIMAGE=planet/base DOCKERFILE=base.dockerfile make-docker-image

# Builds a "buildbox" docker image. Actual building is done inside of Docker, and this
# image is used as a build box. It contains dev tools (Golang, make, git, vi, etc)
buildbox: base
	@echo -e "\n---> Making Planet/BuildBox Docker image to be used for building:\n" ;\
	$(MAKE) -e BUILDIMAGE=planet/buildbox DOCKERFILE=buildbox.dockerfile make-docker-image

# Builds a "testbox" image used during e2e testing.
testbox:
	@echo -e "\n---> Making planet/testbox image for e2e testing:\n" ;\
	$(MAKE) -e BUILDIMAGE=planet/testbox DOCKERFILE=testbox.dockerfile make-docker-image

# removes all build aftifacts 
clean: dev-clean master-clean node-clean test-clean
dev-clean:
	$(MAKE) -C $(ASSETS)/makefiles -e TARGET=dev -f buildbox.mk clean
node-clean:
	$(MAKE) -C $(ASSETS)/makefiles -e TARGET=node -f buildbox.mk clean
master-clean:
	$(MAKE) -C $(ASSETS)/makefiles -e TARGET=master -f buildbox.mk clean
test-clean:
	$(MAKE) -C $(ASSETS)/makefiles -e TARGET=dev -f testbox.mk clean

# internal use:
make-docker-image:
	@if [[ ! $$(docker images | grep $(BUILDIMAGE)) ]]; then \
		cd $(ASSETS)/docker; docker build --no-cache -t $(BUILDIMAGE) -f $(DOCKERFILE) . ;\
	else \
		echo "$(BUILDIMAGE) already exists. Run 'docker rmi $(BUILDIMAGE)' to rebuild" ;\
	fi

remove-godeps:
	rm -rf Godeps/
	find . -iregex .*go | xargs sed -i 's:".*Godeps/_workspace/src/:":g'

prepare-to-run: build
	@sudo mkdir -p /var/planet/registry /var/planet/etcd /var/planet/docker 
	@sudo chown $$USER:$$USER /var/planet/etcd -R
	@cp -f $(BUILDDIR)/current/planet $(BUILDDIR)/current/rootfs/usr/bin/planet

clean-containers:
	@echo -e "\n---> Removing dead Docker/planet containers...\n"
	DEADCONTAINTERS=$$(docker ps --all | grep "planet" | awk '{print $$1}') ;\
	if [ ! -z "$$DEADCONTAINTERS" ] ; then \
		docker rm -f $$DEADCONTAINTERS ;\
	fi

clean-images: clean-containers
	@echo -e "\n---> Removing old Docker/planet images...\n"
	DEADIMAGES=$$(docker images | grep "planet/" | awk '{print $$3}') ;\
	if [ ! -z "$$DEADIMAGES" ] ; then \
		docker rmi -f $$DEADIMAGES ;\
	fi

$(BUILDDIR)/current:
	@echo "You need to build the full image first. Run \"make dev\""
