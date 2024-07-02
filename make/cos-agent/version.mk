VERSION               := $(shell git describe --always --tags --abbrev=8 --dirty)

override LDFLAGS += \
  -X github.com/coscene-io/cos-agent.version=$(VERSION) \
