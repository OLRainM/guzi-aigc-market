FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
ENV GOPROXY=https://goproxy.cn,direct
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/api ./cmd/api \
    && CGO_ENABLED=0 GOOS=linux go build -o /out/migrate ./cmd/migrate

FROM alpine:3.20
RUN apk add --no-cache wget \
    && adduser -D -u 10001 app
USER app
COPY --from=build /out/api /api
COPY --from=build /out/migrate /migrate
EXPOSE 8080
ENTRYPOINT ["/api"]
