# Cluster Reinstallation

You can reinstall a cluster previously installed using the Image Based Install Operator on new hardware while preserving the cluster crypto, identity, and access.

## Prerequisites

- The cluster must have been originally installed using the Image Based Install Operator
- The following secrets must still be present in the hub cluster in the same namespace as the cluster deployment
    - `<clustername>-admin-kubeconfig`
    - `<clustername>-admin-password`
    - `<clustername>-seed-reconfiguration`
- The new machine must have been installed with the desired seed image and ready for final reconfiguration

## Reinstallation

### Backup existing secrets

Execute the following commands to back up the existing secrets where `$CDNAME` is the name of the previous `ClusterDeployment`

```
oc get secret $CDNAME-admin-kubeconfig -oyaml > admin-kubeconfig.yaml
oc get secret $CDNAME-admin-password -oyaml > admin-password.yaml
oc get secret $CDNAME-seed-reconfiguration -oyaml > seed-reconfiguration.yaml
```

### Prepare the secrets for recreation

Once the secrets are backed up some data needs to be removed to ensure they work correctly when recreated.

Remove the following fields if they are present from all the previously created secret files:
- metadata.creationTimestamp
- metadata.labels (remove all except `image-based-installed.openshift.io/created`)
- metadata.ownerReferences
- metadata.resourceVersion
- metadata.uid

The resulting files should look similar to the following examples

#### admin-kubeconfig.yaml

```yaml
apiVersion: v1
data:
  kubeconfig: <base64-encoded-kubeconfig>
kind: Secret
metadata:
  labels:
    image-based-installed.openshift.io/created: "true"
  name: ibi-test-admin-kubeconfig
  namespace: ibi-test
type: Opaque
```

#### admin-password.yaml

```yaml
apiVersion: v1
data:
  password: <base64-encoded-password>
  username: <base64-encoded-username>
kind: Secret
metadata:
  labels:
    image-based-installed.openshift.io/created: "true"
  name: ibi-test-admin-password
  namespace: ibi-test
type: Opaque
```

#### seed-reconfiguration.yaml

```yaml
apiVersion: v1
data:
  manifest.json: <base64-encoded-reconfiguration-manifest>
kind: Secret
metadata:
  labels:
    image-based-installed.openshift.io/created: "true"
  name: ibi-test-seed-reconfiguration
  namespace: ibi-test
type: Opaque
```

### Recreating the cluster

#### Remove the existing cluster resources

Delete the following resources associated with the original cluster installation:
- `ClusterDeployment`
- `ImageClusterInstall`
- `BareMetalHost`
- `DataImage`
- `Secrets`
- `ConfigMaps`

#### Create the secrets from the original installation

```
oc create -f admin-kubeconfig.yaml
oc create -f admin-password.yaml
oc create -f seed-reconfiguration.yaml
```

#### Create the resources to install the cluster

You can trigger cluster installation by recreating the resources the same way they were created for the initial installation.

Note:

Some fields cannot be changed between install and reinstallation. These include:
- `clusterDeployment.baseDomain`
- `clusterDeployment.clusterName`

### Testing reinstallation

Once the cluster has finished installing you can test reinstallation by accessing the cluster using a kubeconfig from the original installation.
If the reinstallation was successful the communication with the cluster will succeed.
