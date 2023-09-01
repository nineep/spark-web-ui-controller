#!/bin/bash

version=$1

docker build --platform linux/amd64 -f Dockerfile -t nineep/spark-web-ui-controller:$version .

docker push nineep/spark-web-ui-controller:$version
