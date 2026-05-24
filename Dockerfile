FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /quartz .

FROM alpine:3.21

RUN addgroup -g 568 quartz && adduser -u 568 -S -G quartz quartz

WORKDIR /app

COPY --from=builder /quartz ./quartz
COPY --chown=quartz:quartz public/ ./public/

RUN mkdir -p /data && chown quartz:quartz /data

# USER quartz

EXPOSE 3000

ENV PORT=3000
ENV DATA_DIR=/data

ENTRYPOINT ["./quartz"]
