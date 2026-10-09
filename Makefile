.DEFAULT_GOAL := help
GO ?= go

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

.PHONY: generate
generate: ## Regenerate Go code from protobuf with buf
	buf dep update
	buf generate

.PHONY: lint
lint: ## buf lint + buf format check + go vet + gofmt check
	buf lint
	buf format --diff --exit-code
	$(GO) vet ./...
	$(GO) vet -tags integration,e2e ./test/...
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './gen/*'))" || (gofmt -l . && exit 1)
	envoy --mode validate -c deploy/envoy/envoy.yaml --log-level error >/dev/null

.PHONY: breaking
breaking: ## Check proto changes against main for breaking changes
	buf breaking --against '.git#branch=main'

.PHONY: test
test: ## Unit tests (no external dependencies)
	$(GO) test -race -count=1 ./...

.PHONY: test-integration
test-integration: ## Integration tests: API + real Temporal dev server + worker + fake TheCatAPI
	$(GO) test -tags integration -race -count=1 ./test/integration/...

.PHONY: test-e2e
test-e2e: ## End-to-end tests through Envoy against the running stack (make up first)
	$(GO) test -tags e2e -count=1 -v ./test/e2e/...

.PHONY: test-all
test-all: lint test test-integration ## Everything that does not need the running stack

.PHONY: up
up: ## Start Temporal, Envoy, worker and API locally
	./scripts/stack.sh up

.PHONY: down
down: ## Stop the local stack
	./scripts/stack.sh down

.PHONY: status
status: ## Show local stack process status
	./scripts/stack.sh status

.PHONY: logs
logs: ## Tail all local stack logs
	./scripts/stack.sh logs
