#!/bin/sh
set -eu
java -cp interop.jar DictionaryFixture
./fix-multiplexer -config config/interop.yaml >/tmp/gateway-metadata.log 2>&1 &
gateway_pid=$!
cleanup() {
  result=$?
  trap - EXIT
  kill -TERM "$gateway_pid" 2>/dev/null || true
  if ! wait "$gateway_pid"; then
    echo 'FAIL: gateway did not stop cleanly' >&2
    result=1
  fi
  exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
java -jar interop.jar
