#!/bin/bash

docker build \
 --build-arg RESOLVER=10.10.10.38 \
 --build-arg AEM_ADDR=$AEM_ADDR \
 --build-arg DOMAIN=$DOMAIN \
 --build-arg S3_ADDR=$S3_ADDR \
 --build-arg STORYLINES_ADDR=$STORYLINES_ADDR \
  -t 056154071827.dkr.ecr.us-east-1.amazonaws.com/$PROJECT_NAME:$GIT_COMMIT-$BUILD_NUMBER ./main

