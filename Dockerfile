FROM node:22-bookworm-slim AS frontend
WORKDIR /build/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.26-bookworm AS server
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY backend/ backend/
COPY frontend/embed.go frontend/
COPY --from=frontend /build/frontend/dist frontend/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /atlas ./backend/cmd/atlas

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /atlas /atlas
ENV ATLAS_ADDR=0.0.0.0:8765 DATA_DIR=/app/data
VOLUME /app/data
EXPOSE 8765
ENTRYPOINT ["/atlas"]
