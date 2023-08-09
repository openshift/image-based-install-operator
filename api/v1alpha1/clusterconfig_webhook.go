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
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var clusterconfiglog = logf.Log.WithName("clusterconfig-resource")

func (r *ClusterConfig) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

//+kubebuilder:webhook:path=/validate-relocation-openshift-io-v1alpha1-clusterconfig,mutating=false,failurePolicy=fail,sideEffects=None,groups=relocation.openshift.io,resources=clusterconfigs,verbs=create;update,versions=v1alpha1,name=vclusterconfig.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &ClusterConfig{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *ClusterConfig) ValidateCreate() (admission.Warnings, error) {
	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *ClusterConfig) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	clusterconfiglog.Info("validate update", "name", r.Name)

	oldConfig, ok := old.(*ClusterConfig)
	if !ok {
		return nil, fmt.Errorf("old object is not a ClusterConfig")
	}

	// return error if BMH ref is set on old, and is still set on r, and an update is happening
	// TODO: should we track the current BMH ref in status so we can unset the image when someone removes the BMH from the object or just make this entirely immutable once you set BMH once?
	if oldConfig.Spec.BareMetalHostRef != nil && r.Spec.BareMetalHostRef != nil {
		return nil, fmt.Errorf("Cannot update ClusterConfig when BareMetalHostRef is set, unset BareMetalHostRef before making changes")
	}

	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *ClusterConfig) ValidateDelete() (admission.Warnings, error) {
	return nil, nil
}
