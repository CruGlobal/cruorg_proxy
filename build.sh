#!/bin/bash

docker build \
 --build-arg RESOLVER=10.10.10.38 \
 --build-arg AEM_LB=stage-cru-org-613775129.us-east-1.elb.amazonaws.com \
 --build-arg DOMAIN=stage.cru.org \
  -t 056154071827.dkr.ecr.us-east-1.amazonaws.com/$PROJECT_NAME:$GIT_COMMIT-$BUILD_NUMBER ./main

