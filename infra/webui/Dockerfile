# syntax=docker/dockerfile:1

# Stage 1: Build
FROM node:24-alpine AS builder

RUN npm install -g pnpm

WORKDIR /app/frontend

COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

COPY frontend/ ./
RUN pnpm build

# Stage 2: Serve. alpine-slim drops the extra nginx modules and tooling, the
# static bundle is all this image needs to run.
FROM nginx:alpine-slim

COPY --from=builder /app/frontend/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
