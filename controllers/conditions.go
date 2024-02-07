package controllers

import (
	"context"
	"time"

	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/openshift/image-based-install-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func findCondition(conditions []hivev1.ClusterInstallCondition, condType hivev1.ClusterInstallConditionType) *hivev1.ClusterInstallCondition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

func setClusterInstallCondition(conditions *[]hivev1.ClusterInstallCondition, newCondition hivev1.ClusterInstallCondition) {
	if conditions == nil {
		return
	}

	now := metav1.NewTime(time.Now())
	existingCondition := findCondition(*conditions, newCondition.Type)
	if existingCondition == nil {
		newCondition.LastTransitionTime = now
		newCondition.LastProbeTime = now
		*conditions = append(*conditions, newCondition)
		return
	}

	if existingCondition.Status != newCondition.Status {
		existingCondition.Status = newCondition.Status
		existingCondition.LastTransitionTime = now
	}

	existingCondition.LastProbeTime = now
	existingCondition.Reason = newCondition.Reason
	existingCondition.Message = newCondition.Message
}

func (r *ImageClusterInstallReconciler) initializeConditions(ctx context.Context, ici *v1alpha1.ImageClusterInstall) error {
	initialTypeStatus := map[hivev1.ClusterInstallConditionType]corev1.ConditionStatus{
		hivev1.ClusterInstallRequirementsMet: corev1.ConditionUnknown,
		hivev1.ClusterInstallCompleted:       corev1.ConditionUnknown,
		hivev1.ClusterInstallFailed:          corev1.ConditionUnknown,
		hivev1.ClusterInstallStopped:         corev1.ConditionFalse,
	}

	patch := client.MergeFrom(ici.DeepCopy())
	for condType, status := range initialTypeStatus {
		if findCondition(ici.Status.Conditions, condType) == nil {
			setClusterInstallCondition(&ici.Status.Conditions, hivev1.ClusterInstallCondition{
				Type:   condType,
				Status: status,
			})
		}
	}

	return r.Status().Patch(ctx, ici, patch)
}

func (r *ImageClusterInstallReconciler) setImageReadyCondition(ctx context.Context, ici *v1alpha1.ImageClusterInstall, err error) error {
	cond := hivev1.ClusterInstallCondition{
		Type:    hivev1.ClusterInstallRequirementsMet,
		Status:  corev1.ConditionTrue,
		Reason:  v1alpha1.ImageReadyReason,
		Message: v1alpha1.ImageReadyMessage,
	}

	if err != nil {
		cond.Status = corev1.ConditionFalse
		cond.Reason = v1alpha1.ImageNotReadyReason
		cond.Message = err.Error()
	}

	patch := client.MergeFrom(ici.DeepCopy())
	setClusterInstallCondition(&ici.Status.Conditions, cond)
	return r.Status().Patch(ctx, ici, patch)
}

func (r *ImageClusterInstallReconciler) setHostConfiguredCondition(ctx context.Context, ici *v1alpha1.ImageClusterInstall, err error) error {
	cond := hivev1.ClusterInstallCondition{
		Type:    hivev1.ClusterInstallStopped,
		Status:  corev1.ConditionFalse,
		Reason:  v1alpha1.HostConfiguraionSucceededReason,
		Message: v1alpha1.HostConfigurationSucceededMessage,
	}

	if err != nil {
		cond.Status = corev1.ConditionTrue
		cond.Reason = v1alpha1.HostConfiguraionFailedReason
		cond.Message = err.Error()
	}

	patch := client.MergeFrom(ici.DeepCopy())
	setClusterInstallCondition(&ici.Status.Conditions, cond)
	return r.Status().Patch(ctx, ici, patch)
}
