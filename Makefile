components = datasplice-core

VERSION ?= SNAPSHOT
env = CGO_ENABLED=0
ldflags = -ldflags "-X main.version=$(VERSION)"
buildcmd = go build $(ldflags) -o ./binaries/$@ .

.PHONY: all build test lint clean install $(components)

all: build

build: $(components)

$(components):
ifeq ($(OS),Windows_NT)
	$(buildcmd)
else
	$(env) $(buildcmd)
endif

test:
	go test -covermode=atomic -coverpkg=./... -shuffle=on ./...

lint:
	golangci-lint run ./...

clean:
ifeq ($(OS),Windows_NT)
	cmd /c if exist binaries rmdir /S /Q binaries
else
	rm -rf ./binaries
endif

install:
	go install $(ldflags) .
