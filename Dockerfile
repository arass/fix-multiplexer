FROM golang:1.27.3 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /fix-multiplexer ./cmd/fix-multiplexer
RUN mkdir -p /state/upstream /state/downstream && chmod 700 /state /state/upstream /state/downstream

FROM build AS test
RUN test -z "$(gofmt -l cmd internal scripts)"
RUN go vet ./...
RUN go test -short -coverprofile=/tmp/coverage.out ./...
RUN go run ./scripts/check-coverage.go /tmp/coverage.out
RUN go test -race -tags=integration ./...
RUN go test -race -tags=soak ./internal/gateway -run TestRoutingSoak -timeout=60s

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /fix-multiplexer /app/fix-multiplexer
COPY config/example.yaml /app/config/example.yaml
COPY spec /app/spec
COPY third_party /app/third_party
COPY --from=build --chown=65532:65532 /state /app/state
ENTRYPOINT ["/app/fix-multiplexer"]
