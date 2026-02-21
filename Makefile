.PHONY: build check format lint test coverage clean

build:
	CGO_ENABLED=0 go build -v -o goscrobble ./...

check: format lint test

format:
	go fmt ./...
	go mod tidy
	find . -name "*.yml" -exec yamlfmt {} +
	find . -name "*.plist" -exec xmllint --format --output {} {} \;
	find . -name "*.sh" -exec shfmt --write {} +

lint:
	golangci-lint run
	find . -name "*.sh" -exec shellcheck {} +

test:
	go test -coverprofile=cover.out -v ./...

coverage: test
	go tool cover -html cover.out

clean:
	go clean
	rm -f goscrobble cover.out
