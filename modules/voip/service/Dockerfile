FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/voip ./main.go

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/voip /usr/local/bin/voip
ENTRYPOINT ["voip"]
