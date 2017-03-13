#!/bin/bash

docker build \
 --build-args RESOLVER=10.10.10.38
 --tag 056154071827.dkr.ecr.us-east-1.amazonaws.com/cruorg_proxy \
 $(dirname $0);

