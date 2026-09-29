.PHONY: all test lint fmt vulncheck

lint:
	$(shell go env GOPATH)/bin/golangci-lint run

fmt:
	go fmt ./...
	$(shell go env GOPATH)/bin/goimports -w .

test:
	go test -race ./...

vulncheck:
	go build -o appmeta ./cmd/appmeta
	$(shell go env GOPATH)/bin/govulncheck -mode=binary ./appmeta
