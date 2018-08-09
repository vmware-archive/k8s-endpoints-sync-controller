GO ?= go
GOVERSION ?= go1.9.2
SHELL := /bin/bash

.DEFAULT_GOAL := build

.PHONY: goversion
goversion: ## Checks if installed go version is latest
	@echo Checking go version...
	@( $(GO) version | grep -q $(GOVERSION) ) || ( echo "Please install $(GOVERSION) (found: $$($(GO) version))" && exit 1 )

.PHONY: build
build: ## Generates the binary
	./build_helper.sh build

.PHONY: buildimage
buildimage: ## builds the docker image
	./build_helper.sh buildimage $$TAG

PHONY: pushimage
pushimage: ## push docker image to registry
	./build_helper.sh pushimage $$TAG $$EXT_TAG
