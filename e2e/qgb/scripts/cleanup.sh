#!/bin/bash

# this script cleans up the docker environment after an unexpected test stop

echo "releasing resources..."

# kill known containers
docker container kill /core0 2> /dev/null
docker container kill /core0-orch 2> /dev/null
docker container kill /core1 2> /dev/null
docker container kill /core1-orch 2> /dev/null
docker container kill /core2 2> /dev/null
docker container kill /core2-orch 2> /dev/null
docker container kill /core3 2> /dev/null
docker container kill /core3-orch 2> /dev/null
docker container kill /relayer 2> /dev/null
docker container kill /deployer 2> /dev/null

# remove known containers
docker container rm /core0 2> /dev/null
docker container rm /core0-orch 2> /dev/null
docker container rm /core1 2> /dev/null
docker container rm /core1-orch 2> /dev/null
docker container rm /core2 2> /dev/null
docker container rm /core2-orch 2> /dev/null
docker container rm /core3 2> /dev/null
docker container rm /core3-orch 2> /dev/null
docker container rm /relayer 2> /dev/null
docker container rm /deployer 2> /dev/null

# handle ganache
ganache_ids=$(docker container ps -a | grep ganache | cut -f 1 -d\ )
for id in $ganache_ids ; do
  echo $id
  docker container kill $id 2> /dev/null
  docker container rm $id 2> /dev/null
done

# remove potential networks that might have been created by docker-compose
potential_networks=$(docker network ls | grep default | cut -f 1 -d\ )
for net in $potential_networks ; do
  echo $net
  docker network rm $net 2> /dev/null
done

echo "done."