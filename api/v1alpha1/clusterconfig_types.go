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
	"github.com/openshift-kni/lifecycle-agent/ibu-imager/clusterinfo"
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

// ClusterConfigSpec defines the desired state of ClusterConfig
type ClusterConfigSpec struct {
	clusterinfo.ClusterInfo `json:",inline"`

	// PullSecretRef is a reference to new cluster-wide pull secret.
	// If defined, it will replace the secret located at openshift-config/pull-secret.
	// The type of the secret must be kubernetes.io/dockerconfigjson.
	PullSecretRef *corev1.LocalObjectReference `json:"pullSecretRef,omitempty"`

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

// ClusterConfigStatus defines the observed state of ClusterConfig
type ClusterConfigStatus struct {
	BareMetalHostRef *BareMetalHostReference `json:"bareMetalHostRef,omitempty"`
	Conditions       []metav1.Condition      `json:"conditions,omitempty"`
}

type BareMetalHostReference struct {
	// Name identifies the BareMetalHost within a namespace
	Name string `json:"name"`
	// Namespace identifies the namespace containing the referenced BareMetalHost
	Namespace string `json:"namespace"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ClusterConfig is the Schema for the clusterconfigs API
type ClusterConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterConfigSpec   `json:"spec,omitempty"`
	Status ClusterConfigStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ClusterConfigList contains a list of ClusterConfig
type ClusterConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterConfig{}, &ClusterConfigList{})
}
