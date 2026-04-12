HOSTNAME=registry.terraform.io
NAMESPACE=alchemaxinc
NAME=balena
BINARY=terraform-provider-${NAME}
VERSION=0.1.0
OS_ARCH=$(shell go env GOOS)_$(shell go env GOARCH)

default: build

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the provider binary
	go build -o ${BINARY}

.PHONY: install
install: build ## Build and install the provider locally
	mkdir -p ~/.terraform.d/plugins/${HOSTNAME}/${NAMESPACE}/${NAME}/${VERSION}/${OS_ARCH}
	mv ${BINARY} ~/.terraform.d/plugins/${HOSTNAME}/${NAMESPACE}/${NAME}/${VERSION}/${OS_ARCH}

.PHONY: test-unit
test-unit: ## Run unit tests
	go test -v -count=1 -timeout 120s ./...

.PHONY: test-integration
test-integration: ## Run integration tests (requires BALENA_API_TOKEN)
	TF_ACC=1 TF_LOG=INFO go test -v -count=1 -timeout 120m -tags=integration ./...

.PHONY: test
test: test-unit ## Alias for test-unit

.PHONY: lint
lint: ## Run linter
	golangci-lint run ./...
	npx prettier --check .

.PHONY: format
format: ## Format all source files with Go fmt and Prettier
	gofmt -s -w .
	npx prettier --write .

.PHONY: fmt
fmt: format ## Alias for format

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: generate
generate: ## Run go generate
	go generate ./...

.PHONY: docs
docs: ## Generate provider documentation
	tfplugindocs generate --provider-name=balena

.PHONY: validate-examples
validate-examples: install ## Validate all example Terraform configs
	@for dir in $$(find examples -name '*.tf' -exec dirname {} \; | sort -u); do \
		echo "Validating $$dir..."; \
		(cd "$$dir" && terraform validate) || exit 1; \
	done

.PHONY: clean
clean: ## Clean up built files
	rm -f ${BINARY}
