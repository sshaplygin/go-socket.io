EXAMPLES := $(patsubst %/go.mod,%,$(wildcard _examples/*/go.mod))
CHAT_SKIP :=
CHAT_COPIES := $(filter-out $(CHAT_SKIP),$(patsubst %/chat.go,%,$(wildcard _examples/*/chat.go)))
LIFECYCLE_COPIES := $(wildcard _examples/*/lifecycle.go)

# Packages of test, test-race, test-stress and bench; CI passes the affected packages
# chosen by .github/changes.
PKGS ?= ./...
# The packages that run goroutines per connection.
STRESS_PKGS := . ./adapter/... ./client/... ./parser ./engineio/...

# Statement coverage and exported-function coverage requirements.
COVER_FLOOR := 91.7
API_UNCOVERED_MAX := 0
# When set, cover-check also keeps its statement profile at this path (CI uploads it).
COVER_PROFILE ?=

.PHONY: all
all:
	go install ./...

.PHONY: test
test:
	go test -count=1 $(PKGS)

.PHONY: test-race
test-race:
	go test -race -count=1 $(PKGS)

# Optional interoperability gate; default Go tests do not require Node. The Redis
# codec oracle decodes a Go-encoded broadcast with the reference server's notepack.io.
.PHONY: conformance
conformance:
	go vet -tags conformance ./internal/conformance/...
	go test -race -count=1 -tags conformance ./internal/conformance/...
	NODE_PATH="$(CURDIR)/internal/conformance/testdata/node/server/node_modules" \
		go test -race -count=1 -run '^TestRedisGoPacketWithNodeOracle$$' ./adapter/redis

.PHONY: apidiff
apidiff:
	bash .github/apidiff/check.sh

.PHONY: test-jsonv2
test-jsonv2:
	GOEXPERIMENT=jsonv2 go test -race -count=1 ./parser/... . ./client/...

# Race tests of STRESS_PKGS that are also in PKGS, repeated and shuffled. The ten runs of
# the root package can exceed go test's default timeout of 10 minutes per package.
.PHONY: test-stress
test-stress:
	@set -e; selected=$$(go list $(PKGS)); \
	pkgs=$$(go list $(STRESS_PKGS) | grep -Fx "$$selected" || true); \
	if test -z "$$pkgs"; then echo "test-stress: no stress package in PKGS"; exit 0; fi; \
	echo go test -race -count=5 -shuffle=on -cpu=1,4 -timeout=20m $$pkgs; \
	go test -race -count=5 -shuffle=on -cpu=1,4 -timeout=20m $$pkgs

# Race tests of STRESS_PKGS repeated under CPU load: STRESS_LOAD POSIX sh busy loops (twice the
# CPU count) run while go test -json writes STRESS_JSON, which .github/stress summarizes into
# STRESS_SUMMARY and stdout. The exit status is that of go test. The traps kill the loops on
# exit, failure and interruption: a non-interactive sh starts them with SIGINT ignored.
STRESS_COUNT ?= 20
STRESS_CPU ?= 1,4
STRESS_TIMEOUT ?= 90m
STRESS_LOAD ?= $(shell n=$$(nproc 2>/dev/null || sysctl -n hw.ncpu); echo $$((n * 2)))
STRESS_JSON ?= stress.json
STRESS_SUMMARY ?= stress-summary.md

.PHONY: test-stress-load
test-stress-load:
	@set -e; pids=; i=0; \
	trap 'kill $$pids 2>/dev/null' EXIT; trap 'exit 130' INT; trap 'exit 143' TERM; \
	while test $$i -lt $(STRESS_LOAD); do sh -c 'while :; do :; done' & pids="$$pids $$!"; i=$$((i+1)); done; \
	echo "$(STRESS_LOAD) busy loops; go test -race -json -count=$(STRESS_COUNT) -shuffle=on -cpu=$(STRESS_CPU) -timeout=$(STRESS_TIMEOUT) $(STRESS_PKGS) >$(STRESS_JSON)"; \
	status=0; go test -race -json -count=$(STRESS_COUNT) -shuffle=on -cpu=$(STRESS_CPU) -timeout=$(STRESS_TIMEOUT) $(STRESS_PKGS) >$(STRESS_JSON) || status=$$?; \
	go run ./.github/stress -json $(STRESS_JSON) -summary $(STRESS_SUMMARY) || true; \
	exit $$status

# Browser tests: the js/wasm tests of internal/browsertest and engineio/transport/browser
# and the client's transport substitution, run in headless Chrome by wasmbrowsertest
# against the native fixture server of internal/browsertest/server. The server serves
# until its standard input, a FIFO held open on descriptor 3, closes on exit, failure or
# interruption. go test runs with -v, so that the browser console of passing packages
# reaches the output too, and writes to a file, because POSIX sh has no pipefail; a
# JavaScript failure in that output also fails the target. wasmbrowsertest hands its whole
# environment to the wasm program, whose wasm_exec.js fails when arguments and environment
# pass about 8 KB, so go test runs with only the variables that it and the tests need.
# BROWSER_COVER receives the statement profile of engineio/transport/browser, which
# cover-check cannot see.
WASMBROWSERTEST_VERSION := v0.11.0
BROWSER_COUNT ?= 1
BROWSER_COVER ?= browser-cover.out
BROWSER_PKGS := ./internal/browsertest/... ./engineio/transport/browser/...

.PHONY: install-wasmbrowsertest
install-wasmbrowsertest:
	go install github.com/agnivade/wasmbrowsertest@$(WASMBROWSERTEST_VERSION)

.PHONY: test-browser
test-browser:
	@set -eu; \
	runner=$$(command -v wasmbrowsertest) || { echo "test-browser: wasmbrowsertest is not on PATH; run make install-wasmbrowsertest" >&2; exit 1; }; \
	tmp=$$(mktemp -d); pid=; \
	trap 'exec 3>&-; test -z "$$pid" || wait $$pid || true; rm -f "$$tmp/stdin" "$$tmp/server.log" "$$tmp/test.log"; rmdir "$$tmp"' EXIT; \
	trap 'exit 130' INT; trap 'exit 143' TERM; \
	mkfifo "$$tmp/stdin"; \
	go run -tags browsertest ./internal/browsertest/server <"$$tmp/stdin" >"$$tmp/server.log" 2>&1 & pid=$$!; \
	exec 3>"$$tmp/stdin"; \
	i=0; until grep -q '^SOCKETIO_BROWSER_CLOSED_URL=' "$$tmp/server.log"; do \
		if ! kill -0 $$pid 2>/dev/null; then \
			cat "$$tmp/server.log" >&2; echo "test-browser: the server exited before it printed its URLs" >&2; exit 1; fi; \
		i=$$((i + 1)); if test $$i -gt 300; then \
			cat "$$tmp/server.log" >&2; echo "test-browser: the server printed no URL within 30 s" >&2; exit 1; fi; \
		sleep 0.1; done; \
	SOCKETIO_BROWSER_URL=$$(sed -n 's/^SOCKETIO_BROWSER_URL=//p' "$$tmp/server.log"); \
	SOCKETIO_BROWSER_CLOSED_URL=$$(sed -n 's/^SOCKETIO_BROWSER_CLOSED_URL=//p' "$$tmp/server.log"); \
	browser_test() { env -i PATH="$$PATH" HOME="$$HOME" TMPDIR="$${TMPDIR:-/tmp}" \
		GOCACHE="$$(go env GOCACHE)" GOMODCACHE="$$(go env GOMODCACHE)" GOPATH="$$(go env GOPATH)" \
		GOPROXY="$$(go env GOPROXY)" GOFLAGS="$$(go env GOFLAGS)" GOTOOLCHAIN="$$(go env GOTOOLCHAIN)" \
		GOOS=js GOARCH=wasm SOCKETIO_BROWSER_URL="$$SOCKETIO_BROWSER_URL" \
		SOCKETIO_BROWSER_CLOSED_URL="$$SOCKETIO_BROWSER_CLOSED_URL" \
		SOCKETIO_BROWSER_LEAK="$${SOCKETIO_BROWSER_LEAK:-}" go test "$$@"; }; \
	status=0; \
	browser_test -v -count=$(BROWSER_COUNT) -timeout 5m -exec "$$runner" -coverpkg=./engineio/transport/browser \
		-coverprofile="$(BROWSER_COVER)" $(BROWSER_PKGS) >"$$tmp/test.log" 2>&1 3>&- || status=$$?; \
	browser_test -v -count=$(BROWSER_COUNT) -timeout 5m -exec "$$runner" -run '^TestPlatformTransport' \
		./client >>"$$tmp/test.log" 2>&1 3>&- || status=$$?; \
	cat "$$tmp/test.log"; \
	if grep -E 'call to released function|Go program has already exited|Uncaught' "$$tmp/test.log" >/dev/null; then \
		echo "test-browser: JavaScript error in the test output" >&2; test $$status -ne 0 || status=1; fi; \
	if test $$status -ne 0; then echo "==> server output" >&2; cat "$$tmp/server.log" >&2; fi; \
	test ! -s "$(BROWSER_COVER)" || go tool cover -func="$(BROWSER_COVER)" | tail -n 1; \
	exit $$status

.PHONY: bench
bench:
	go test -run '^$$' -bench . -benchmem $(PKGS)

.PHONY: lint
lint: vet lint-go
	gofmt -s -l . | tee /dev/stderr | test -z "$$(cat)"

# golangci-lint of the root module, the packages that ./... skips (the Go client example,
# the Engine.IO example, the README quick-start fixture and the .github tools), the
# js/wasm files of the browser client and its tests, the browsertest-tagged fixture server
# and every example module with the root configuration. The CI lint job runs this target
# after its own Format step.
.PHONY: lint-go
lint-go:
	golangci-lint run ./... ./_examples/client ./engineio/_examples ./internal/parity/testdata/quickstart \
		./.github/architecture ./.github/changes ./.github/stress
	GOOS=js GOARCH=wasm golangci-lint run ./client/... ./engineio/... ./internal/browsertest/...
	golangci-lint run --build-tags browsertest ./internal/browsertest/...
	@set -e; for d in $(EXAMPLES); do echo "==> golangci-lint $$d"; \
		(cd $$d && golangci-lint run --config "$(CURDIR)/.golangci.yml" ./...); done

# go vet of every Go file that a CI job compiles, so that a change of comments alone, which
# runs only the lint job, cannot fail a job that the change skips: the root module under
# each GOOS of the test matrix (a file excluded on linux, the GOOS of the lint job, is
# checked only by its own GOOS), the conformance-tagged tests, the js/wasm files of the
# browser client and its tests, the browsertest-tagged fixture server, the example modules
# and engineio/_examples (vetted again by make examples), and the .github tools.
.PHONY: vet
vet:
	go vet ./.github/architecture ./.github/changes ./.github/stress
	@set -e; for os in linux darwin windows; do echo "==> GOOS=$$os"; GOOS=$$os go vet ./...; done
	GOOS=linux GOARCH=386 go vet ./...
	go vet -tags conformance ./...
	GOOS=js GOARCH=wasm go vet ./client/... ./engineio/... ./internal/browsertest/...
	go vet -tags browsertest ./internal/browsertest/...
	@set -e; for d in $(EXAMPLES); do echo "==> $$d"; (cd $$d && go vet ./...); done
	go vet ./engineio/_examples

# Runtime package boundaries are defined in docs/ARCHITECTURE.md.
.PHONY: architecture-check
architecture-check:
	go test -race -count=1 ./.github/architecture
	go run ./.github/architecture
	go run ./.github/architecture -goos js -goarch wasm

.PHONY: vuln
vuln:
	set -e; \
	govulncheck ./...; \
	for d in _examples/*/go.mod; do \
		(cd "$$(dirname "$$d")" && govulncheck ./...); \
	done

.PHONY: cover
cover:
	go test -count=1 -coverprofile=c.out ./... && go tool cover -html=c.out

# Public consumer/role contracts, library lifecycle policy and the runtime graph.
.PHONY: gates
gates: architecture-check
	go test -race -count=1 ./internal/parity

.PHONY: cover-check
cover-check:
	@set -eu; \
	profile=$$(mktemp); summary=$$(mktemp); annotated=$$(mktemp); \
	trap 'rm -f "$$profile" "$$summary" "$$annotated"' 0; \
	go test -count=1 -coverpkg=./... -coverprofile="$$profile" ./...; \
	if [ -n "$(COVER_PROFILE)" ]; then cp "$$profile" "$(COVER_PROFILE)"; fi; \
	go tool cover -func="$$profile" >"$$summary"; \
	go test -count=1 ./.github/coverage; \
	go run ./.github/coverage -profile "$$profile" -summary "$$summary" -root . -module "$$(go list -m)" >"$$annotated"; \
	awk -v floor="$(COVER_FLOOR)" -v max="$(API_UNCOVERED_MAX)" '\
		BEGIN { \
			if (floor !~ /^[0-9]+([.][0-9]+)?$$/ || floor+0 > 100 || max !~ /^[0-9]+$$/) { \
				print "cover-check: invalid COVER_FLOOR or API_UNCOVERED_MAX"; exit 1; \
			} \
		} \
		$$1 == "total:" { totals++; value=$$3; sub(/%$$/, "", value); total=value+0 } \
		$$3 == "0.0%" && $$2 ~ /^[A-Z]/ { uncovered++; print "uncovered API: " $$1 " " $$2 } \
		END { \
			if (totals != 1 || value !~ /^[0-9]+([.][0-9]+)?$$/) { print "cover-check: missing coverage total"; exit 1 } \
			printf "coverage: %.1f%% (floor %s%%); uncovered API: %d (maximum %s)\n", total, floor, uncovered, max; \
			if (total < floor+0 || uncovered > max+0) exit 1; \
		}' "$$annotated"

.PHONY: examples
examples:
	@set -e; for d in $(EXAMPLES); do echo "==> $$d"; (cd $$d && go build -o /dev/null ./... && go vet ./...); done
	go build -o /dev/null ./_examples/client
	go build -o /dev/null ./engineio/_examples
	go vet ./engineio/_examples
	go test -race -count=1 ./engineio/_examples
	@set -e; for d in $(CHAT_COPIES); do cmp $$d/chat.go _examples/default-http/chat.go; done
	@set -e; for f in $(LIFECYCLE_COPIES); do cmp $$f _examples/default-http/lifecycle.go; done
	cd _examples/default-http && go test -race -count=1 ./...
	cd _examples/gin-cors && go test -race -count=1 ./...

.PHONY: examples-node
# CONFORMANCE_NODE_MODULES selects the pinned socket.io-client used by the runner.
examples-node:
	bash _examples/run-node.sh

# Foundation files stay independent copies in both modules.
.PHONY: sync-check
sync-check:
	@set -eu; \
	tmp=$$(mktemp); trap 'rm -f "$$tmp"' 0; \
	while read -r root_file mirror_file extra || test -n "$$root_file"; do \
		case "$$root_file" in ''|\#*) continue ;; esac; \
		if test -z "$$mirror_file" || test -n "$$extra"; then \
			echo "sync-check: expected two paths: $$root_file $$mirror_file $$extra" >&2; exit 1; \
		fi; \
		sed 's#github[.]com/sshaplygin/go-socket[.]io/v2#github.com/sshaplygin/go-socket.io#g' "$$mirror_file" > "$$tmp"; \
		cmp "$$root_file" "$$tmp" || { \
			echo "sync-check: shared files differ: $$root_file $$mirror_file" >&2; exit 1; \
		}; \
	done < .github/sync-manifest
