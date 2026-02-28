#!/bin/bash
set -e

echo "Building and starting services..."
docker compose up --build -d

echo "Services started. Press Ctrl+C to stop..."
trap "docker compose down" EXIT

docker compose logs -f
