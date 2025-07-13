FROM golang:1.24-alpine AS builder

RUN apk add --no-cache gcc musl-dev ffmpeg

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .


RUN go build -o /voice-shut-up-bot .

FROM alpine:3.22

RUN apk add --no-cache ffmpeg

WORKDIR /app

COPY --from=builder /voice-shut-up-bot .

CMD ["./voice-shut-up-bot"]