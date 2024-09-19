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

echo "Waiting for deleting data image after deletion of ici"
oc get -n ibi-test dataimage ostest-extraworker
oc delete imageclusterinstalls.extensions.hive.openshift.io -n ibi-test ibi-test --wait=false

for i in {1..30}; do
  res=$(oc get -n ibi-test dataimage ostest-extraworker 2>&1 || true)
  echo $res | grep NotFound
  ret=$?
  if [[ $ret -eq 0 ]]; then
    echo "DataImage was removed successfully"
    break
  fi
  echo "Found existing DataImage, waiting for delete ($i)..."
  sleep 1
done

if [[ $ret -ne 0 ]]; then
  echo "ERROR - Failed tp remove DataImage on ICI deletion"
  exit 1
fi
