# Stage 1 - build frontend
FROM node:22 AS frontend-build

WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/. .
RUN npm run build

# Stage 2 - build go binary
FROM golang:1.26 AS go-build

WORKDIR /app
COPY backend/go.mod backend/go.sum ./backend/
RUN cd backend && go mod download
COPY . .
COPY --from=frontend-build /app/backend/internal/webui/dist ./backend/internal/webui/dist

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /app/backend
RUN test -f internal/webui/dist/index.html && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -tags embed_frontend \
    -trimpath -ldflags="-s -w -X main.version=$VERSION" -o /notepadtt .
RUN mkdir /data

# Stage 3 - minimal runtime image (ripgrep is shelled out to for workspace search)
FROM alpine:3.20
RUN apk add --no-cache ripgrep

COPY --from=go-build /notepadtt /notepadtt
COPY --from=go-build /data /data

VOLUME /data
EXPOSE 8080

ENTRYPOINT ["/notepadtt", "-d", "/data"]
