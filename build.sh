#!/bin/bash

docker buildx build $DOCKER_ARGS \
 --build-arg RESOLVER="10.16.2.22 10.16.3.22" \
 --build-arg AEM_ADDR=$AEM_ADDR \
 --build-arg DOMAIN=$DOMAIN \
 --build-arg S3_ADDR=$S3_ADDR \
 --build-arg STORYLINES_ADDR=$STORYLINES_ADDR \
 --build-arg REDIS_PORT_6379_TCP_ADDR_A=$REDIS_PORT_6379_TCP_ADDR_A \
 --build-arg REDIS_PORT_6379_TCP_ADDR_PS=$REDIS_PORT_6379_TCP_ADDR_PS \
 ./main
