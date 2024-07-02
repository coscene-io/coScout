MAKEGO := make/go
MAKEGO_REMOTE := https://github.com/coscene-io/cos-agent.git
PROJECT := cos-agent
GO_MODULE := github.com/coscene-io/cos-agent
DOCKER_ORG := cosceneio
DOCKER_PROJECT := cos-agent

include make/cos-agent/all.mk
include make/cos-agent/version.mk
