VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build install test vet dist clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/mds .

install:
	go install -ldflags "$(LDFLAGS)" .

test:
	go test ./...

vet:
	go vet ./...

dist:
	scripts/build.sh $(VERSION)

clean:
	rm -rf bin dist
