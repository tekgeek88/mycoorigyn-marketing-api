FROM golang:1.25.0-alpine AS build

ARG ENV=production
ENV ENV=${ENV}

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mycoorigyn-marketing-api ./cmd/server

FROM alpine:3.21

ARG ENV=production
ENV ENV=${ENV}

RUN addgroup -S app && adduser -S app -G app
RUN apk add --no-cache ca-certificates tzdata

USER app

COPY --from=build /out/mycoorigyn-marketing-api /usr/local/bin/mycoorigyn-marketing-api

EXPOSE 8080
ENTRYPOINT ["mycoorigyn-marketing-api"]
