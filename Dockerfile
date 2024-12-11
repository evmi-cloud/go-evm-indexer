FROM golang:1.22.5 as builder
ARG CGO_ENABLED=0
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN cd cmd/evm-indexer && go build


FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/cmd/evm-indexer/evm-indexer /evm-indexer
ENTRYPOINT ["/evm-indexer"]