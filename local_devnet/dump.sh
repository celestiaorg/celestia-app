docker compose logs core0 | sed -r 's/\x1b\[[0-9;]+m//g' > core0.log
docker compose logs core1 | sed -r 's/\x1b\[[0-9;]+m//g' > core1.log
docker compose logs core2 | sed -r 's/\x1b\[[0-9;]+m//g' > core2.log
docker compose logs core3 | sed -r 's/\x1b\[[0-9;]+m//g' > core3.log
