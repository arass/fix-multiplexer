FROM maven:3.9.9-eclipse-temurin-21 AS java
WORKDIR /fixture
COPY interop/quickfixj/pom.xml ./
RUN mvn -q dependency:go-offline
COPY interop/quickfixj/src ./src
RUN mvn -q package

FROM golang:1.27.3 AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false -o /fix-multiplexer ./cmd/fix-multiplexer
RUN mkdir -p /state/upstream /state/downstream && chmod 700 /state /state/upstream /state/downstream

FROM eclipse-temurin:21-jre
WORKDIR /work
COPY --from=go /fix-multiplexer ./fix-multiplexer
COPY --from=java /fixture/target/interop.jar ./interop.jar
COPY config ./config
COPY interop/quickfixj/config.yaml ./config/interop.yaml
COPY spec ./spec
COPY third_party ./third_party
COPY THIRD_PARTY_NOTICES.md ./THIRD_PARTY_NOTICES.md
COPY --from=go --chown=65532:65532 /state ./state
COPY interop/quickfixj/run.sh ./run.sh
USER 65532:65532
ENTRYPOINT ["sh", "./run.sh"]
