GO_BINS := $(GO_BINS) cmd/cos

LICENSE_HEADER_LICENSE_TYPE := apache
LICENSE_HEADER_COPYRIGHT_HOLDER := coScene
LICENSE_HEADER_YEAR_RANGE := 2025
LICENSE_HEADER_IGNORES := \/testdata

include make/go/bootstrap.mk
include make/go/go.mk

.PHONY: build-binary
build-binary:
	go build -ldflags '${LDFLAGS}' -o ./bin/cos ./cmd/coscout/main.go
