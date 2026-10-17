# syntax=docker/dockerfile:1.7
FROM golang:1.23-alpine AS build
ARG VERSION=1.0.0
ENV CGO_ENABLED=0 GOFLAGS="-trimpath"
RUN go build -ldflags="-s -w" -o /out/app ./cmd/app \
    && chmod +x /out/app
COPY go.mod go.sum /src/
WORKDIR /src
LABEL org.opencontainers.image.version="1.0.0" org.opencontainers.image.title=app
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/out/app"]
CMD /out/app --serve
