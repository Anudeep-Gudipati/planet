.PHONY: all

GOPATH := /gopath
REPODIR := $(GOPATH)/src/github.com/kubernetes/kubernetes
VER := e3188f6ee7007000c5daf525c8cc32b4c5bf4ba8

BINARIES := $(TARGETDIR)/kube-apiserver \
	$(TARGETDIR)/kube-controller-manager \
	$(TARGETDIR)/kube-scheduler \
	$(TARGETDIR)/kubectl \
	$(TARGETDIR)/kube-proxy \
	$(TARGETDIR)/kubelet \
	$(TARGETDIR)/test/ginkgo \
	$(TARGETDIR)/test/e2e.test

all: kubernetes-dev.mk $(BINARIES)

$(BINARIES): 
$(BINARIES):
	@echo "\n---> Building Kubernetes\n"
	mkdir -p $(GOPATH)/src/github.com/kubernetes
	cd $(GOPATH)/src/github.com/kubernetes && git clone https://github.com/kubernetes/kubernetes
	cd $(REPODIR) && git checkout $(VER)
	$(REPODIR)/hack/build-go.sh
	cp $(REPODIR)/_output/local/bin/linux/amd64/kube* $(TARGETDIR)/
	cp $(REPODIR)/_output/local/bin/linux/amd64/ginkgo $(TARGETDIR)/test/
	cp $(REPODIR)/_output/local/bin/linux/amd64/e2e.test $(TARGETDIR)/test/
