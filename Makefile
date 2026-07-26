.PHONY: build test lint lint-ci verify generate webui e2e-live

CUSTOM_LINT ?= ./custom-golangci-lint

custom-golangci-lint: .custom-gcl.yml
	golangci-lint custom

build:
	go build ./...

test:
	go test ./...

lint: custom-golangci-lint
	$(CUSTOM_LINT) run ./...

lint-ci: lint

verify: test lint

generate:
	buf generate

webui:
	go test ./webui

e2e-live:
	go test -tags=live ./test/live -count=1 -timeout=10m
