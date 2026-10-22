FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/voip ./main.go

FROM alpine:3.20
# ffmpeg turns an uploaded greeting (mp3, m4a, ...) into what Asterisk plays.
RUN apk add --no-cache ca-certificates ffmpeg
COPY --from=build /out/voip /usr/local/bin/voip
ENTRYPOINT ["voip"]
