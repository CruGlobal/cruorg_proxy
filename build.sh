#!/bin/bash

docker build \
 --build-arg RESOLVER=10.10.10.38 \
  -t 056154071827.dkr.ecr.us-east-1.amazonaws.com/$PROJECT_NAME:$GIT_COMMIT-$BUILD_NUMBER ./main

