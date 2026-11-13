# Empty starts every database. Use make db-up DATABASES=postgres for just one.
DATABASES ?=
ALL_DATABASES := postgres,mysql,mariadb,sqlite,sqlserver
# TESTS picks the packages the database targets run, as CI does to split them
# into shards, such as TESTS=./tests/matrix. Left empty, test-pg and
# test-integration run every package under tests, and the targets of the other
# databases run DIALECT_TESTS: the feature examples run on PostgreSQL alone, so
# elsewhere they would only compile and skip.
TESTS ?=
DIALECT_TESTS = $(shell go list -f '{{.ImportPath}} {{join .TestImports " "}} {{join .XTestImports " "}}' ./tests/... | awk '/internal\/testdb/ {print $$1}')

# PARALLEL is how many tests of a package run at once. Each test has a database
# of its own and mostly waits on it, so more than the CPUs pays.
PARALLEL ?= 8

# integration runs the database tests on the packages given, unless TESTS says
# otherwise.
integration = go test -count=1 -parallel $(PARALLEL) $(TESTFLAGS) $(or $(TESTS),$(1))
MODULES := . tools/antvet tools/antfmt tools/compare contrib/otel
NESTED_MODULES := $(filter-out .,$(MODULES))

.PHONY: build tidy deps bench test test-modules lint vet staticcheck fmt db-up db-down dialect-tests test-integration test-pg test-mysql test-mariadb test-sqlite test-mssql

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

# PACKAGES picks what make test runs in the root module, as CI does to split it
# across runners. Left at ./..., make test also runs test-modules, since ./...
# stops at the nested modules.
PACKAGES ?= ./...

test:
	go test -parallel $(PARALLEL) $(TESTFLAGS) $(PACKAGES)
	$(if $(filter ./...,$(PACKAGES)),@$(MAKE) --no-print-directory test-modules)

# test-modules runs the tests of the nested modules.
test-modules:
	@for module in $(NESTED_MODULES); do (cd $$module && go test $(TESTFLAGS) ./...) || exit 1; done

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

# lint is vet and staticcheck, which CI runs side by side.
lint: vet staticcheck

vet:
	go vet ./...
	go build -C tools/antvet -o $(ANTVET) ./cmd/antvet
	go vet -vettool=$(ANTVET) ./...
	go run -C tools/antfmt . -l $(CURDIR)
	@for module in $(MODULES); do (cd $$module && golangci-lint run ./...) || exit 1; done

staticcheck:
	staticcheck ./...

db-up:
	docker compose up -d --wait $(DATABASES)

db-down:
	docker compose down -v

# dialect-tests prints DIALECT_TESTS, which CI splits into shards.
dialect-tests:
	@printf '%s\n' $(DIALECT_TESTS)

test-integration:
	ANT_TEST_DATABASES=$(ALL_DATABASES) $(call integration,./tests/...)

test-pg:
	ANT_TEST_DATABASES=postgres $(call integration,./tests/...)

test-mysql:
	ANT_TEST_DATABASES=mysql $(call integration,$(DIALECT_TESTS))

test-mariadb:
	ANT_TEST_DATABASES=mariadb $(call integration,$(DIALECT_TESTS))

test-sqlite:
	ANT_TEST_DATABASES=sqlite $(call integration,$(DIALECT_TESTS))

test-mssql:
	ANT_TEST_DATABASES=sqlserver $(call integration,$(DIALECT_TESTS))
