#!/usr/bin/env bash
echo "Deploying"

for i in $(seq 1 100); do
	curl -s -X POST "http://localhost:8080/deploy/$i" &
	sleep 0.1
done

echo
echo "Stopping"

for i in $(seq 1 100); do
	curl -s -X POST "http://localhost:8080/stop/$i"
done