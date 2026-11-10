BINARY  := terraform-provider-onetsolutions
VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)
GOBIN   ?= $(shell go env GOPATH)/bin

.PHONY: build install fmt fmt-check vet lint vulncheck test test-tofu testacc sweep docs docs-check tf-fmt tf-fmt-check check all

## build: compile the provider binary into ./terraform-provider-onetsolutions
build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) .

## install: install the provider into GOBIN, for dev_overrides
install:
	go install -trimpath -ldflags "$(LDFLAGS)" .

## fmt: format Go code and examples
fmt:
	gofmt -s -w .
	terraform fmt -recursive examples

## fmt-check: fail when Go code is not formatted
fmt-check:
	@unformatted="$$(gofmt -s -l .)"; \
	if [ -n "$$unformatted" ]; then echo "gofmt needed on:"; echo "$$unformatted"; exit 1; fi

vet:
	go vet ./...

lint:
	golangci-lint run ./...

## vulncheck: known vulnerabilities reachable from the code (govulncheck)
vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

## test: unit tests, provider tests against the in-process fake API and the contract test.
## They need no credentials and order nothing.
test:
	go test -race -count=1 ./...

## test-tofu: the same tests, with OpenTofu running the configurations (needs tofu on PATH)
test-tofu:
	TF_ACC_TERRAFORM_PATH="$$(command -v tofu)" TF_ACC_PROVIDER_HOST=registry.opentofu.org go test -count=1 ./...

## testacc: acceptance tests against the REAL API. They ORDER BILLED RESOURCES: run them by hand
## against a test project with the smallest monthly size, never in CI. They need
## ONETSOLUTIONS_API_KEY, ONETSOLUTIONS_ORGANIZATION_ID and ONETSOLUTIONS_PROJECT_ID.
testacc:
	TF_ACC=1 go test -count=1 -timeout 120m ./...

## sweep: delete the tf-acc-* objects interrupted acceptance runs left in the test project, on the
## REAL API, with the credentials of testacc. Run by hand only.
sweep:
	go test ./internal/sweep -v -count=1 -sweep=all -timeout 60m

DOCS_FLAGS := --provider-name onetsolutions --rendered-provider-name OnetSolutions

## docs: regenerate docs/ from the schema, examples/ and templates/
docs:
	go tool tfplugindocs generate $(DOCS_FLAGS)
	go tool tfplugindocs validate --provider-name onetsolutions

## docs-check: fail when docs/ differs from what `make docs` would generate
docs-check:
	@rm -rf .docs-check
	@go tool tfplugindocs generate $(DOCS_FLAGS) --rendered-website-dir .docs-check > /dev/null 2>&1 || \
		go tool tfplugindocs generate $(DOCS_FLAGS) --rendered-website-dir .docs-check
	@if ! diff -r docs .docs-check; then rm -rf .docs-check; echo "docs/ is out of date: run make docs"; exit 1; fi
	@rm -rf .docs-check
	@go tool tfplugindocs validate --provider-name onetsolutions > /dev/null 2>&1 || \
		go tool tfplugindocs validate --provider-name onetsolutions
	@echo "docs/ is up to date"

tf-fmt:
	terraform fmt -recursive examples

tf-fmt-check:
	terraform fmt -check -recursive examples

## check: everything CI runs
check: fmt-check vet lint test docs-check tf-fmt-check

## all: format, then check
all: fmt check
