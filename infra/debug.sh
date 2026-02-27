cd manager && ./build.sh && cd ..
cd worker && ./build.sh && cd ..

docker compose up --build
docker compose down
