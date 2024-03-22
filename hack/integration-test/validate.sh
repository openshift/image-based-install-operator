#!/bin/bash

set -ex

echo "Waiting for configuration ISO URL to be set on ImageClusterInstall ibi-test/ibi-test"

for i in {1..60}; do
  url=$(oc get -n ibi-test imageclusterinstall ibi-test -ojsonpath='{.status.configurationImageURL}')
  if [[ -n "$url" ]]; then
    break
  fi
  sleep 1
done

if [[ -z "$url" ]]; then
  echo "ERROR: configurationImageURL on ImageClusterInstall ibi-test/ibi-test was not set within 60 seconds"
  exit 1
fi

curl --insecure --output "test.iso" "$url"
file "test.iso" | grep -q "ISO 9660 CD-ROM"
