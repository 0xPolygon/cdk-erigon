FROM golang:1.24.4 as builder
WORKDIR /app
COPY . .

RUN make all

FROM debian:trixie-slim
COPY --from=builder /app/build/bin/geth /usr/local/bin/