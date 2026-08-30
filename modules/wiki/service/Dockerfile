FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/wiki ./main.go

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/wiki /usr/local/bin/wiki
ENTRYPOINT ["wiki"]
