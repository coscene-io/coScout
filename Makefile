MAKEGO := make/go
MAKEGO_REMOTE := https://github.com/coscene-io/coScout.git
PROJECT := coscout
GO_MODULE := github.com/coscene-io/coscout
GO_MOD_VERSION := 1.26.5
DOCKER_ORG := cosceneio
DOCKER_PROJECT := coscout

include make/coscout/all.mk
include make/coscout/version.mk
