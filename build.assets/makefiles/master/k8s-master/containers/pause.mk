.PHONY: all export pull-from-internet

IMAGE:=gcr.io/google_containers/pause:0.8.0
# OUTDIR defines the output directory for the resulting tarball
# (set in the parent makefile)
OUT:=$(OUTDIR)/pause.tar.gz

all: pull-from-internet $(OUT)

$(OUT):
	@echo "Exporting image to file system..."
	docker save -o $@ $(IMAGE)

# TODO: make this target the result of `docker ps | grep nettest`
pull-from-internet:
	@echo "Pulling docker image..."
	docker pull $(IMAGE)
