.PHONY: build test lint lint-ci verify generate webui coverage e2e e2e-live e2e-live-mlx

CUSTOM_LINT ?= ./custom-golangci-lint

# Minimum scoped coverage over hand-written Go production code. Ratchet this
# value rather than editing scripts/go-coverage.sh.
GO_COVERAGE_MIN ?= 60

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
	cd webui && pnpm run build

coverage:
	GO_COVERAGE_MIN=$(GO_COVERAGE_MIN) ./scripts/go-coverage.sh

# Provision the pinned llama.cpp build and GGUF model, then run the compiled
# process E2E against them. Provisioning failure is fatal by design.
e2e:
	eval "$$(./scripts/provision-e2e-llamacpp.sh)" && \
		go test -tags=e2e ./test/e2e -count=1 -timeout=15m

e2e-live:
	go test -tags=live ./test/live -count=1 -timeout=10m

analyze:
	go tool sizeanalyzer -html report.html