#!/bin/bash

set -ex

echo "Waiting for configuration ISO URL to be set on ImageClusterInstall ibi-test/ibi-test"

for i in {1..120}; do
  url=$(oc get -n ibi-test dataimage ostest-extraworker -ojsonpath='{.spec.url}' || true)
  if [[ -n "$url" ]]; then
    break
  fi
  sleep 1
done

if [[ -z "$url" ]]; then
  echo "ERROR: configurationImageURL on ImageClusterInstall ibi-test/ibi-test was not set within 60 seconds"
  exit 1
fi
