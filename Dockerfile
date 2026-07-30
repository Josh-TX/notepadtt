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
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend-build /app/frontend/dist ./frontend/dist

ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
    -trimpath -ldflags="-s -w" -o /notepadtt .
RUN mkdir /data

# Stage 3 - minimal runtime image
FROM gcr.io/distroless/static-debian12

COPY --from=go-build /notepadtt /notepadtt
COPY --from=go-build /data /data

VOLUME /data
EXPOSE 8080

ENTRYPOINT ["/notepadtt", "-d", "/data"]
