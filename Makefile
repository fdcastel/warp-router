.PHONY: build rootfs lxc qcow2 test test-integration clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev-$(shell git rev-parse --short HEAD)")
BINARY  := warp
BUILDDIR := build
ROOTFSDIR := $(BUILDDIR)/rootfs
LXC_OUTPUT := $(BUILDDIR)/warp-router-$(VERSION)-lxc-amd64.tar.zst
QCOW2_OUTPUT := $(BUILDDIR)/warp-router-$(VERSION)-amd64.qcow2

## Build the warp binary (static, linux/amd64)
build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(BUILDDIR)/$(BINARY) ./cmd/warp/

## Build the Debian 13 rootfs
rootfs:
	sudo packaging/rootfs/build-rootfs.sh $(ROOTFSDIR)

## Build LXC template from rootfs
lxc: rootfs
	packaging/lxc/build-lxc.sh $(ROOTFSDIR) $(LXC_OUTPUT)

## Build QCOW2 image from rootfs
qcow2: rootfs
	packaging/qcow2/build-qcow2.sh $(ROOTFSDIR) $(QCOW2_OUTPUT)

## Run unit tests
test:
	go test ./...

## Run integration tests (requires Proxmox env vars)
test-integration:
	go test -tags integration -timeout 30m ./test/integration/...

## Remove build artifacts
clean:
	rm -rf $(BUILDDIR)
