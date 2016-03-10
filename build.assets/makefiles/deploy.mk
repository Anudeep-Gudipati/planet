# Deploy build artifacts to Amazon S3
#
.PHONY: all deploy

TARGETS=master node
# Prefix used to place build artifacts on Amazon S3
# The complete URL would be s3://${DEPLOY_BUCKET_PREFIX}/${BUILD_TAG}/planet-${TARGET}.tar.gz
# with BUILD_TAG derived from `git describe`
BUILD_BUCKET_URL=s3://builds.gravitational.io/planet
BUILD_BUCKET_REGION=us-east-1
BUILD_TAG=$(shell git describe)

all: deploy

deploy:
	@echo "Deploying $(BUILD_TAG) to Amazon S3"
	$(foreach target,\
		$(TARGETS),\
		aws s3 cp $(BUILDDIR)/$(target)/planet-$(target).tar.gz $(BUILD_BUCKET_URL)/$(BUILD_TAG)/ --region=$(BUILD_BUCKET_REGION);)
