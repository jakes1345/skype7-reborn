FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/nexus-server .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/nexus-server /app/nexus-server
COPY templates /app/templates
COPY public /app/public

ENV PORT=8080
ENV DB_PATH=/data/nexus.db
EXPOSE 8080

CMD ["/app/nexus-server"]
