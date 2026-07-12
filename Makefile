APP := skubell
BIN_DIR := bin
PKG := ./cmd/skubell
INTEGRATION_PKG := ./...
INTEGRATION_TAGS := -tags integration
TEST_API_MIN := http://localhost:8081/api.php
TEST_API_LATEST := http://localhost:8082/api.php

.PHONY: build test run
.PHONY: test-fast test-all
.PHONY: test-integration test-integration-min test-integration-latest

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(APP) $(PKG)

test:
	$(MAKE) test-fast

test-fast:
	go test ./internal/...

test-all:
	go test ./...

test-integration-min:
	SKUBELL_TEST_API=$(TEST_API_MIN) go test $(INTEGRATION_TAGS) $(INTEGRATION_PKG)

test-integration-latest:
	SKUBELL_TEST_API=$(TEST_API_LATEST) go test $(INTEGRATION_TAGS) $(INTEGRATION_PKG)

test-integration: test-integration-min test-integration-latest

run:
	./bin/$(APP)
