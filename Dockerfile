FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/gaccel-node ./cmd/server

FROM alpine:3.22

RUN adduser -D -H -s /sbin/nologin gaccel
WORKDIR /app

COPY --from=build /out/gaccel-node /usr/local/bin/gaccel-node
COPY config.example.yaml /app/config.example.yaml

USER gaccel
EXPOSE 443/udp 9090/tcp

ENTRYPOINT ["gaccel-node"]
CMD ["-config", "/app/config.yaml"]
