VERSION               ?= $(shell git describe --always --tags --abbrev=8 --dirty)

override LDFLAGS += \
  -X github.com/coscene-io/coscout.version=$(VERSION) \
