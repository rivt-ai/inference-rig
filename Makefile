.PHONY: build test lint verify e2e-live

build:
	go build ./...

test:
	go test ./...

lint:
	golangci-lint run ./...

verify: test lint

# Live end-to-end tests against real engines are added with the backends
# (Phase 6/7) and hardware validation (Phase 12). Placeholder keeps the
# ported Live E2E workflow valid until then.
e2e-live:
	@echo "no live e2e yet (added with the backends)"
