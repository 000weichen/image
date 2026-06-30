#!/bin/sh
set -e

: "${IMGBED_CONFIG:=/app/config.yaml}"
export IMGBED_CONFIG

mkdir -p /app/data /app/uploads
created_config=0

if [ -d "$IMGBED_CONFIG" ]; then
	echo "config path is a directory, expected a file: $IMGBED_CONFIG" >&2
	exit 1
fi

if [ ! -f "$IMGBED_CONFIG" ]; then
	cp /app/config.example.yaml "$IMGBED_CONFIG"
	created_config=1
fi

chown -R imgbed:imgbed /app/data /app/uploads 2>/dev/null || true
if [ "$created_config" = "1" ]; then
	chown imgbed:imgbed "$IMGBED_CONFIG" 2>/dev/null || true
fi

exec su-exec imgbed "$@"
