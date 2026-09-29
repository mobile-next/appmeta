.PHONY: all test lint fmt vulncheck

all:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o appmeta ./cmd/appmeta

lint:
	$(shell go env GOPATH)/bin/golangci-lint run

fmt:
	go fmt ./...
	$(shell go env GOPATH)/bin/goimports -w .

test:
	go test -race ./...

vulncheck: all
	$(shell go env GOPATH)/bin/govulncheck -mode=binary ./appmeta
