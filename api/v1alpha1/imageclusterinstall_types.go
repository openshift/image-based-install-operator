/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ImageReadyCondition = "ImageReady"
	ImageNotReadyReason = "NotReady"
	ImageReadyReason    = "Ready"
	ImageReadyMessage   = "Image is ready for use"

	HostConfiguredCondition           = "HostConfigured"
	HostConfiguraionFailedReason      = "HostConfigurationFailed"
	HostConfiguraionSucceededReason   = "HostConfigurationSucceeded"
	HostConfigurationSucceededMessage = "Configuration image is attached to the referenced host"
)

// ImageClusterInstallSpec defines the desired state of ImageClusterInstall
type ImageClusterInstallSpec struct {
	// ClusterDeploymentRef is a reference to the ClusterDeployment.
	// +optional
	ClusterDeploymentRef *corev1.LocalObjectReference `json:"clusterDeploymentRef"`

	// ImageSetRef is a reference to a ClusterImageSet.
	ImageSetRef hivev1.ClusterImageSetReference `json:"imageSetRef"`

	// ClusterMetadata contains metadata information about the installed cluster.
	// This must be set as soon as all the information is available.
	// +optional
	ClusterMetadata *hivev1.ClusterMetadata `json:"clusterMetadata"`

	// Version is the target OCP version for the cluster
	// TODO: should this use ImageSetRef?
	Version string `json:"version,omitempty"`

	// MasterIP is the desired IP for the host
	MasterIP string `json:"masterIP,omitempty"`

	// Hostname is the desired hostname for the host
	Hostname string `json:"hostname,omitempty"`

	// CABundle is a reference to a config map containing the new bundle of trusted certificates for the host.
	// The tls-ca-bundle.pem entry in the config map will be written to /etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem
	CABundleRef *corev1.LocalObjectReference `json:"caBundleRef,omitempty"`

	// NetworkConfigRef is the reference to a config map containing network configuration files if necessary.
	// Keys should be of the form *.nmconnection and each represent an nmconnection file to be applied to the host.
	// +optional
	NetworkConfigRef *corev1.LocalObjectReference `json:"networkConfigRef,omitempty"`

	// ExtraManifestsRefs is list of config map references containing additional manifests to be applied to the relocated cluster.
	// +optional
	ExtraManifestsRefs []corev1.LocalObjectReference `json:"extraManifestsRef,omitempty"`

	// BareMetalHostRef identifies a BareMetalHost object to be used to attach the configuration to the host.
	// +optional
	BareMetalHostRef *BareMetalHostReference `json:"bareMetalHostRef,omitempty"`
}

// ImageClusterInstallStatus defines the observed state of ImageClusterInstall
type ImageClusterInstallStatus struct {
	// Conditions is a list of conditions associated with syncing to the cluster.
	// +optional
	Conditions []hivev1.ClusterInstallCondition `json:"conditions,omitempty"`

	// InstallRestarts is the total count of container restarts on the clusters install job.
	InstallRestarts int `json:"installRestarts,omitempty"`

	BareMetalHostRef *BareMetalHostReference `json:"bareMetalHostRef,omitempty"`
	ConfigConditions []metav1.Condition      `json:"configConditions,omitempty"`
}

type BareMetalHostReference struct {
	// Name identifies the BareMetalHost within a namespace
	Name string `json:"name"`
	// Namespace identifies the namespace containing the referenced BareMetalHost
	Namespace string `json:"namespace"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ImageClusterInstall is the Schema for the imageclusterinstall API
type ImageClusterInstall struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ImageClusterInstallSpec   `json:"spec,omitempty"`
	Status ImageClusterInstallStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ImageClusterInstallList contains a list of ImageClusterInstall
type ImageClusterInstallList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ImageClusterInstall `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ImageClusterInstall{}, &ImageClusterInstallList{})
}
