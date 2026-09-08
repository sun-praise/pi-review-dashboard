# 前端 → 静态资源
FROM node:22-bookworm-slim AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# Go → 静态二进制（modernc.org/sqlite 纯 Go，CGO 关掉也能编）
# 1.25 对齐 go.mod：modernc.org/sqlite v1.58 要求 Go >= 1.25
FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist web/dist
RUN CGO_ENABLED=0 go build -trimpath -o /out/pi-review-dashboard .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/pi-review-dashboard /pi-review-dashboard
VOLUME /data
ENV STATS_DB=/data/stats.db
EXPOSE 8787
ENTRYPOINT ["/pi-review-dashboard"]
