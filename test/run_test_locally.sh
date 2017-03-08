#!/bin/bash

# Clean up anything that might be lingering from a prior run
docker-compose down

# Make sure we have the latest build then start up the stack
docker-compose build && docker-compose up -d

sleep 1
echo ''

bundle install
ruby tests.rb
