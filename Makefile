SHELL := /bin/bash
VERSION := $(shell tr -d '[:space:]' < VERSION)
DIST_DIR ?= dist
PORT ?= 8080

.PHONY: all build test check browser-check serve package clean

all: check build

build:
	./scripts/build-static.sh "$(DIST_DIR)"

test:
	go test ./...
	node --test tests/js/*.test.mjs

check:
	./scripts/check.sh

browser-check: build
	npm run test:browser

serve: build
	python3 -m http.server "$(PORT)" --bind 127.0.0.1 --directory "$(DIST_DIR)"

package: check
	./scripts/package-release.sh

clean:
	rm -rf dist release
