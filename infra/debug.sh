#!/bin/bash
set -e

cleanup() {
    echo "Cleaning up..."
    docker compose down
}

trap cleanup EXIT

echo "Building and starting services..."
docker compose up --build
