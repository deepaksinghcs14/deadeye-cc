.PHONY: build test vet fmt check clean gen-catalog

BINARY := deadeye
PKG    := ./cmd/deadeye

build:
	go build -o bin/$(BINARY) $(PKG)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	@files="$$(gofmt -l .)"; \
	if [ -n "$$files" ]; then \
		echo "gofmt needs to be run on:"; echo "$$files"; exit 1; \
	fi

check: vet fmt test

gen-catalog:
	go run scripts/gen-catalog.go

clean:
	rm -rf bin
