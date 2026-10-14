# syntax=docker/dockerfile:1

FROM node:24-alpine AS frontend-build

WORKDIR /src/frontend

RUN corepack enable

COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

COPY frontend/ ./
RUN pnpm build

FROM golang:1.26.5-alpine AS backend-build

WORKDIR /src/backend

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/openlist2emby ./cmd/server

FROM alpine:3.22 AS runtime

RUN apk add --no-cache ca-certificates ffmpeg tzdata font-noto-cjk \
    && addgroup -S openlist \
    && adduser -S -D -H -G openlist openlist \
    && mkdir -p /app/frontend /data \
    && chown -R openlist:openlist /app /data

COPY --from=backend-build --chown=openlist:openlist /out/openlist2emby /app/openlist2emby
COPY --from=frontend-build --chown=openlist:openlist /src/frontend/dist/ /app/frontend/

USER openlist
WORKDIR /app

ENV OPENLIST2EMBY_ADDR=:8091 \
    OPENLIST2EMBY_DB=/data/openlist2emby.db

EXPOSE 8091 8096
VOLUME ["/data"]

ENTRYPOINT ["/app/openlist2emby"]
CMD ["-static", "/app/frontend"]
