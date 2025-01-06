MAKEGO := make/go
MAKEGO_REMOTE := https://github.com/coscene-io/cos-agent.git
PROJECT := coscout
GO_MODULE := github.com/coscene-io/coscout
DOCKER_ORG := cosceneio
DOCKER_PROJECT := coscout

include make/coscout/all.mk
include make/coscout/version.mk
