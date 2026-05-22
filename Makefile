.PHONY: all
all: vet test build

.PHONY: build
build:
	go build ./cmd/tflocalexpand

.PHONY: vet
vet:
	go vet ./...

TEST_FLAGS ?=

.PHONY: test
test:
	go test -v -count=1 $(TEST_FLAGS) ./...

.PHONY: lint
lint:
	golangci-lint run
