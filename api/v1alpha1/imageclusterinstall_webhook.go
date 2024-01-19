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
	"reflect"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var icilog = logf.Log.WithName("imageclusterinstall-resource")

func (r *ImageClusterInstall) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

var _ webhook.Validator = &ImageClusterInstall{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *ImageClusterInstall) ValidateCreate() (admission.Warnings, error) {
	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *ImageClusterInstall) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	icilog.Info("validate update", "name", r.Name)

	oldClusterInstall, ok := old.(*ImageClusterInstall)
	if !ok {
		return nil, fmt.Errorf("old object is not an ImageClusterInstall")
	}

	if oldClusterInstall.Spec.BareMetalHostRef == nil && r.Spec.BareMetalHostRef == nil {
		return nil, nil
	}

	// Allow update if it's not the spec
	if !isSpecUpdate(oldClusterInstall, r) {
		return nil, nil
	}
	if BMHRefsMatch(oldClusterInstall.Spec.BareMetalHostRef, r.Spec.BareMetalHostRef) {
		return nil, fmt.Errorf("Cannot update ImageClusterInstall when BareMetalHostRef is set, unset BareMetalHostRef before making changes")
	}
	return nil, nil
}

func isSpecUpdate(oldClusterInstall *ImageClusterInstall, newClusterInstall *ImageClusterInstall) bool {
	oldSpec := oldClusterInstall.Spec.DeepCopy()
	newSpec := newClusterInstall.Spec.DeepCopy()

	return !reflect.DeepEqual(oldSpec, newSpec)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *ImageClusterInstall) ValidateDelete() (admission.Warnings, error) {
	return nil, nil
}

func BMHRefsMatch(ref1 *BareMetalHostReference, ref2 *BareMetalHostReference) bool {
	if ref1 == nil && ref2 == nil {
		return true
	}

	if ref1 == nil && ref2 != nil || ref1 != nil && ref2 == nil {
		return false
	}

	return *ref1 == *ref2
}
