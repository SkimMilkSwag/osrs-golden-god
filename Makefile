# OSRS Golden God — behavior layer for an OSRS bot experiment.
#
# The build is plain `go build` (see README); the Makefile exists for the
# convenience targets, not as a required toolchain.

BINARY := osrs-golden-god

.PHONY: all build test vet fmt clean demo plan

all: build

build:
	go build -o $(BINARY) .

test:
	go test ./... -count=1

vet:
	go vet ./...

fmt:
	gofmt -w .

# Quick local sanity check of the two subcommands (deterministic seeds).
demo: build
	./$(BINARY) demo

plan: build
	./$(BINARY) plan --min-hours 2 --max-hours 6 --sessions 3 --seed 7

clean:
	rm -f $(BINARY)
