BIN_DIR := bin
DIST_DIR := dist
VERSION ?= dev
LDFLAGS := -X gitlab.babun.tv/nfs4/nfs4-tools/nfs4viv/appinfo.Version=$(VERSION)

DIST_FILES := README.md LICENSE
LINUX_ARCHIVE := $(DIST_DIR)/nfs4viv-$(VERSION)-linux-amd64.tar.gz
WINDOWS_ARCHIVE := $(DIST_DIR)/nfs4viv-$(VERSION)-windows-amd64.zip

.PHONY: all build windows test dist clean

all: build

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/nfs4viv ./cmd/nfs4viv

windows:
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/nfs4viv.exe ./cmd/nfs4viv

test:
	go test -count=1 ./...

# Release archives: the binary plus the README and the license. Linux ships a
# tar.gz, Windows a zip; cmd/packdist writes both without external tools.
dist: build windows
	rm -rf $(DIST_DIR)/stage
	mkdir -p $(DIST_DIR)/stage/linux $(DIST_DIR)/stage/windows
	cp $(BIN_DIR)/nfs4viv $(DIST_DIR)/stage/linux/
	cp $(BIN_DIR)/nfs4viv.exe $(DIST_DIR)/stage/windows/
	cp $(DIST_FILES) $(DIST_DIR)/stage/linux/
	cp $(DIST_FILES) $(DIST_DIR)/stage/windows/
	go run ./cmd/packdist $(LINUX_ARCHIVE) $(DIST_DIR)/stage/linux/nfs4viv $(DIST_DIR)/stage/linux/README.md $(DIST_DIR)/stage/linux/LICENSE
	go run ./cmd/packdist $(WINDOWS_ARCHIVE) $(DIST_DIR)/stage/windows/nfs4viv.exe $(DIST_DIR)/stage/windows/README.md $(DIST_DIR)/stage/windows/LICENSE
	rm -rf $(DIST_DIR)/stage

clean:
	rm -rf $(BIN_DIR) $(DIST_DIR)
