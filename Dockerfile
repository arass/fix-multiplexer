FROM golang:1.27.3 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /fix-multiplexer ./cmd/fix-multiplexer

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /fix-multiplexer /app/fix-multiplexer
COPY config/example.yaml /app/config/example.yaml
COPY spec /app/spec
COPY third_party /app/third_party
ENTRYPOINT ["/app/fix-multiplexer"]
