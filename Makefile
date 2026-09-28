EXAMPLES := $(patsubst %/go.mod,%,$(wildcard _examples/*/go.mod))

.PHONY: all
all:
	go install ./...

.PHONY: test
test:
	go test -count=1 ./...

.PHONY: test-race
test-race:
	go test -race -count=1 ./...

.PHONY: bench
bench:
	go test -run '^$$' -bench . -benchmem ./...

.PHONY: lint
lint:
	gofmt -s -l . | tee /dev/stderr | test -z "$$(cat)"
	go vet ./...
	golangci-lint run ./...

.PHONY: vuln
vuln:
	govulncheck ./...

.PHONY: cover
cover:
	go test -count=1 -coverprofile=c.out ./... && go tool cover -html=c.out

.PHONY: examples
examples:
	@set -e; for d in $(EXAMPLES); do echo "==> $$d"; (cd $$d && go build -o /dev/null ./...); done
