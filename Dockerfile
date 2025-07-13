FROM golang:1.24-alpine AS builder

WORKDIR /app

RUN apk add --no-cache gcc musl-dev ffmpeg

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o /voice-shut-up-bot .

FROM alpine:3.22

WORKDIR /app

RUN apk add --no-cache ffmpeg

RUN adduser -D -u 1001 nonroot

COPY --from=builder /voice-shut-up-bot .

RUN chown -R nonroot:nonroot /app

USER nonroot

CMD ["./voice-shut-up-bot"]