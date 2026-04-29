FROM golang:1.24-alpine AS builder

RUN apk add --no-cache ca-certificates git

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/server ./cmd/server

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /out/server /app/server
COPY static /app/static

RUN mkdir -p /app/tmp/repos

ENV PORT=8090
ENV WORK_DIR=/app/tmp/repos

EXPOSE 8090

CMD ["/app/server"]
