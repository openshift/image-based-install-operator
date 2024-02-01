package controllers

import (
	"context"

	"github.com/openshift/image-based-install-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *ImageClusterInstallReconciler) setImageReadyCondition(ctx context.Context, ici *v1alpha1.ImageClusterInstall, err error) error {
	cond := metav1.Condition{
		Type:    v1alpha1.ImageReadyCondition,
		Status:  metav1.ConditionTrue,
		Reason:  v1alpha1.ImageReadyReason,
		Message: v1alpha1.ImageReadyMessage,
	}

	if err != nil {
		cond.Status = metav1.ConditionFalse
		cond.Reason = v1alpha1.ImageNotReadyReason
		cond.Message = err.Error()
	}

	patch := client.MergeFrom(ici.DeepCopy())
	meta.SetStatusCondition(&ici.Status.ConfigConditions, cond)
	return r.Status().Patch(ctx, ici, patch)
}

func (r *ImageClusterInstallReconciler) setHostConfiguredCondition(ctx context.Context, ici *v1alpha1.ImageClusterInstall, err error) error {
	cond := metav1.Condition{
		Type:    v1alpha1.HostConfiguredCondition,
		Status:  metav1.ConditionTrue,
		Reason:  v1alpha1.HostConfiguraionSucceededReason,
		Message: v1alpha1.HostConfigurationSucceededMessage,
	}

	if err != nil {
		cond.Status = metav1.ConditionFalse
		cond.Reason = v1alpha1.HostConfiguraionFailedReason
		cond.Message = err.Error()
	}

	patch := client.MergeFrom(ici.DeepCopy())
	meta.SetStatusCondition(&ici.Status.ConfigConditions, cond)
	return r.Status().Patch(ctx, ici, patch)
}
