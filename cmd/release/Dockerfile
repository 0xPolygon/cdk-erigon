FROM docker.io/library/golang:1.23-alpine3.21 AS builder
WORKDIR /app

ADD go.mod go.mod
ADD go.sum go.sum
RUN go mod download

ADD . .

ARG GITHUB_TOKEN

RUN go build -o releaser main.go
RUN ./releaser --github-token=$GITHUB_TOKEN

FROM nginx:1.21.3-alpine

# Set proper permissions for nginx user
COPY --chown=nginx:nginx page /usr/share/nginx/html
COPY --chown=nginx:nginx nginx.conf /etc/nginx/conf.d/default.conf
COPY --chown=nginx:nginx --from=builder /app/releases.json /usr/share/nginx/html/data.json

USER nginx

# Explicitly set permissions for the data directory
RUN chmod -R 755 /usr/share/nginx/html
