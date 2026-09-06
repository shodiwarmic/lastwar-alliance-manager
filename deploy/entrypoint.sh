#!/bin/sh
set -e
# Fix ownership of bind-mounted volumes so appuser can read/write them
chown -R appuser:appuser /app/data /app/uploads
exec gosu appuser "$@"
