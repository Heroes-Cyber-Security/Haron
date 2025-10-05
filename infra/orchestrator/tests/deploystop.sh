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

for i in $(seq 301 400); do
	curl -s -X POST "http://localhost:8080/deploy/$i" &
done

sleep 30

for i in $(seq 401 500); do
	curl -s -X POST "http://localhost:8080/deploy/$i" &
done

sleep 30

echo
echo "Stopping"

for i in $(seq 1 500); do
	curl -s -X POST "http://localhost:8080/stop/$i"
done