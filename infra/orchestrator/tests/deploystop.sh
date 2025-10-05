#!/usr/bin/env bash
echo "Deploying"

for i in $(seq 1 100); do
	curl -s -X POST "http://localhost:8080/deploy/$i" &
done

sleep 30

for i in $(seq 101 200); do
	curl -s -X POST "http://localhost:8080/deploy/$i" &
done

sleep 30

for i in $(seq 201 300); do
	curl -s -X POST "http://localhost:8080/deploy/$i" &
done

sleep 30

echo
echo "Stopping"

for i in $(seq 1 300); do
	curl -s -X POST "http://localhost:8080/stop/$i"
done