# Empty starts every database. Use make db-up DATABASES=postgres for just one.
DATABASES ?=
ALL_DATABASES := postgres,mysql,mariadb,sqlite,sqlserver
# TESTS picks the packages the database targets run, as CI does to split them
# into shards, such as TESTS=./tests/matrix.
TESTS ?= ./tests/...

# PARALLEL is how many tests of a package run at once. Each test has a database
# of its own and mostly waits on it, so more than the CPUs pays.
PARALLEL ?= 8
INTEGRATION = go test -count=1 -parallel $(PARALLEL) $(TESTFLAGS) $(TESTS)
MODULES := . tools/antvet tools/antfmt tools/compare contrib/otel

.PHONY: build tidy deps bench test lint fmt db-up db-down test-integration test-pg test-mysql test-mariadb test-sqlite test-mssql

build:
	@for module in $(MODULES); do (cd $$module && go build -o /dev/null ./...) || exit 1; done

tidy:
	@for module in $(MODULES); do (cd $$module && go mod tidy) || exit 1; done

# deps checks every module for vulnerabilities that its code reaches, which
# fails, then lists direct dependencies with a newer version, which does not.
GOVULNCHECK ?= go run golang.org/x/vuln/cmd/govulncheck@latest

deps:
	@for module in $(MODULES); do (cd $$module && $(GOVULNCHECK) ./...) || exit 1; done
	@for module in $(MODULES); do \
		out=$$(cd $$module && go list -u -m -f '{{if and .Update (not .Indirect) (not .Replace)}}{{.Path}} {{.Version}} -> {{.Update.Version}}{{end}}' all) || exit 1; \
		printf '%s\n' "$$out" | sed -n "s|^.|$$module: &|p"; \
	done

# TESTFLAGS passes flags through, as CI does with make test TESTFLAGS="-v -race -failfast".
TESTFLAGS ?=

# PACKAGES picks what make test runs, as CI does to split it across runners.
PACKAGES ?= ./...

test:
	go test -parallel $(PARALLEL) $(TESTFLAGS) $(PACKAGES)

bench:
	go test -run '^$$' -bench . -benchtime 2s ./tests/benchmarks/

# fmt lays the code out, in two rounds because one tool's line breaks can give the
# other something to do. lint fails on anything fmt would change.
fmt:
	@for round in 1 2; do \
		go run -C tools/antfmt . $(CURDIR) || exit 1; \
		for module in $(MODULES); do (cd $$module && golangci-lint run --fix ./...) || exit 1; done; \
	done

# ANTVET is where lint builds the analyzer, out of the source tree.
ANTVET := $(shell go env GOCACHE)/antvet-lint

lint:
	go vet ./...
	go build -C tools/antvet -o $(ANTVET) ./cmd/antvet
	go vet -vettool=$(ANTVET) ./...
	staticcheck ./...
	go run -C tools/antfmt . -l $(CURDIR)
	@for module in $(MODULES); do (cd $$module && golangci-lint run ./...) || exit 1; done

db-up:
	docker compose up -d --wait $(DATABASES)

db-down:
	docker compose down -v

test-integration:
	ANT_TEST_DATABASES=$(ALL_DATABASES) $(INTEGRATION)

test-pg:
	ANT_TEST_DATABASES=postgres $(INTEGRATION)

test-mysql:
	ANT_TEST_DATABASES=mysql $(INTEGRATION)

test-mariadb:
	ANT_TEST_DATABASES=mariadb $(INTEGRATION)

test-sqlite:
	ANT_TEST_DATABASES=sqlite $(INTEGRATION)

test-mssql:
	ANT_TEST_DATABASES=sqlserver $(INTEGRATION)
