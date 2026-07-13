FROM node:22-alpine AS frontend
WORKDIR /build/web
COPY web/package.json web/package-lock.json* ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS backend
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /build/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /vek ./cmd/vek

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=backend /vek /usr/local/bin/vek
EXPOSE 8659
VOLUME /data
ENV VEKTOR_DATA_DIR=/data
ENTRYPOINT ["vek"]
CMD ["serve"]
