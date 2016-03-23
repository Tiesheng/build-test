DOCKER ?= $(shell which docker)

all:
	CGO_ENABLED=0 go build -a -installsuffix cgo -o builder .

image:
	$(DOCKER) build -t chengtiesheng/builder .

.PHONY: test .gofmt .govet .golint

test: .gofmt .govet .golint

.gofmt:
		go fmt ./...

.govet:
		go vet -x ./...

.golint:
		golint ./...
