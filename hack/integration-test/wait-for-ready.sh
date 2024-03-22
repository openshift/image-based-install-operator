#!/bin/bash

namespace="image-based-install-operator"
selector="app=image-based-install-operator"

echo "Waiting for pod in namespace ${namespace} with labels ${selector} to be created..."
for i in {1..40}; do
  oc get pod --selector="${selector}" --namespace=${namespace} |& grep -ivE "(no resources found|not found)" && break || sleep 10
done

echo "Waiting for pod in namespace ${namespace} with labels ${selector} to become Ready..."
oc wait -n "${namespace}" --for=condition=Ready --selector "${selector}" pod --timeout=10m
