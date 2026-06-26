GO_FILES := $(shell git ls-files '*.go')
GOLANGCI_LINT_VERSION := v2.12.2
GOLANGCI_LINT_GOTOOLCHAIN ?= $(shell go env GOVERSION)
GOLANGCI_LINT := GOTOOLCHAIN=$(GOLANGCI_LINT_GOTOOLCHAIN) go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
GOIMPORTS := goimports

.PHONY: check check-goimports fmt fmt-check lint vet

check: fmt-check vet lint

check-goimports:
	@command -v $(GOIMPORTS) >/dev/null

fmt:
	@command -v $(GOIMPORTS) >/dev/null
	gofmt -w $(GO_FILES)
	$(GOIMPORTS) -w -local github.com/mattsp1290/eino-agui $(GO_FILES)

fmt-check: check-goimports
	@test -z "$$(gofmt -l $(GO_FILES))"
	@test -z "$$($(GOIMPORTS) -l -local github.com/mattsp1290/eino-agui $(GO_FILES))"

vet:
	go vet ./...

lint:
	$(GOLANGCI_LINT) run ./...
