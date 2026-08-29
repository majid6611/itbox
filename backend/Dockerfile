FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache docker-cli docker-cli-compose ca-certificates
COPY --from=build /out/server /usr/local/bin/server
ENTRYPOINT ["server"]
