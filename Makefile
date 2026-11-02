.PHONY: fmt vet lint test integration race coverage soak check smoke
fmt:
	go fmt ./...
vet:
	go vet ./...
lint: vet
test:
	go test -short ./...
integration:
	go test -tags=integration ./...
race:
	go test -race ./...
coverage:
	go test -short -coverprofile=coverage.out ./...
	go run ./scripts/check-coverage.go coverage.out
soak:
	go test -race -tags=soak ./internal/gateway -run TestRoutingSoak -timeout=60s
check: fmt vet test race coverage
smoke:
	docker build -t fix-multiplexer .
	docker run --rm fix-multiplexer -check-config
	docker run --rm fix-multiplexer -demo
