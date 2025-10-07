$(cd manager && ./build.sh)
$(cd worker && ./build.sh)

docker compose up
docker compose down
