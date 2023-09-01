#!/bin/sh
# entrypoint.sh
# echo commands to the terminal output
set -ex
exec /sbin/tini -s -- /usr/bin/spark-web-ui-controller "$@"
