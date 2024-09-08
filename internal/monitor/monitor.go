package monitor

import (
	"context"
	"fmt"
	"strings"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	nodesReadyMessage                 = "All nodes are ready"
	clusterVersionAvailableMessage    = "ClusterVersion is available"
	clusterVersionNotAvailableMessage = "ClusterVersion is not yet available due to stale data"
	IBIOStartTimeCM                   = "ibi-monitor-cm"
	OcpConfigNamespace                = "openshift-config"
)

type ClusterInstallStatus struct {
	Installed            bool
	ClusterVersionStatus string
	NodesStatus          string
}

func (status *ClusterInstallStatus) String() string {
	installStatus := "installing"
	if status.Installed {
		installStatus = "installed"
	}
	return fmt.Sprintf("Cluster is %s\nClusterVersion Status: %s\nNodes Status: %s", installStatus, status.ClusterVersionStatus, status.NodesStatus)
}

type GetInstallStatusFunc func(ctx context.Context, log logrus.FieldLogger, c client.Client) ClusterInstallStatus

func getIBIOStartTime(ctx context.Context, c client.Client) (metav1.Time, error) {
	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, types.NamespacedName{Name: IBIOStartTimeCM, Namespace: OcpConfigNamespace}, cm); err != nil {
		return metav1.Time{}, err
	}

	return cm.CreationTimestamp, nil
}

func GetClusterInstallStatus(ctx context.Context, log logrus.FieldLogger, c client.Client) ClusterInstallStatus {
	reconfigurationStartTime, err := getIBIOStartTime(ctx, c)
	if err != nil {
		return ClusterInstallStatus{
			Installed:            false,
			ClusterVersionStatus: fmt.Sprintf("Failed to get %s : %s", IBIOStartTimeCM, err),
		}
	}
	cvAvailable, cvMessage, err := clusterVersionStatus(ctx, log, c, reconfigurationStartTime)
	if err != nil {
		cvMessage = fmt.Sprintf("Failed to check cluster version status: %s", err)
		return ClusterInstallStatus{
			Installed:            false,
			ClusterVersionStatus: cvMessage,
		}
	}

	nodesReady, nodesMessage, err := nodesStatus(ctx, log, c)
	if err != nil {
		nodesMessage = fmt.Sprintf("Failed to check node status: %s", err)
	}

	return ClusterInstallStatus{
		Installed:            cvAvailable && nodesReady,
		ClusterVersionStatus: cvMessage,
		NodesStatus:          nodesMessage,
	}
}

func clusterVersionStatus(ctx context.Context, log logrus.FieldLogger, c client.Client, reconfigurationStartTime metav1.Time) (bool, string, error) {
	cv := &configv1.ClusterVersion{}
	if err := c.Get(ctx, types.NamespacedName{Name: "version"}, cv); err != nil {
		return false, "", err
	}

	for _, cond := range cv.Status.Conditions {
		if cond.Type == configv1.OperatorAvailable {
			if !didCVOStarted(log, cv, reconfigurationStartTime) {
				log.Infof(clusterVersionNotAvailableMessage)
				return false, clusterVersionNotAvailableMessage, nil
			}
			if cond.Status == configv1.ConditionTrue {
				return true, clusterVersionAvailableMessage, nil
			}
			if cond.Type == configv1.OperatorAvailable {
				message := fmt.Sprintf("ClusterVersion is not yet available because %s: %s", cond.Reason, cond.Message)
				log.Infof(message)
				return false, message, nil
			}
		}
	}

	return false, "ClusterVersion Available condition not found", nil
}

// didCVOStarted checks if the ClusterVersionOperator has started to run by updating at least one of its conditions
// by comparing the last transition time of the conditions with the reconfiguration start time taken from the  ConfigMap
// We check all the conditions in order to find at least one updated
func didCVOStarted(log logrus.FieldLogger, cvo *configv1.ClusterVersion, reconfigurationStartTime metav1.Time) bool {
	startTime := reconfigurationStartTime.Add(-60 * time.Minute)
	log.Infof("Checking if CVO has started and updated at least one of its conditions after %s", startTime)
	for _, cond := range cvo.Status.Conditions {
		if cond.LastTransitionTime.After(startTime) {
			return true
		}
	}
	return false
}

func nodesStatus(ctx context.Context, log logrus.FieldLogger, c client.Client) (bool, string, error) {
	nodes := &corev1.NodeList{}
	if err := c.List(ctx, nodes); err != nil {
		return false, "", err
	}
	if len(nodes.Items) == 0 {
		message := "No nodes found"
		log.Info(message)
		return false, message, nil
	}

	nodesReady := true
	messages := make([]string, 0)
	for _, node := range nodes.Items {
		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeReady {
				if cond.Status != corev1.ConditionTrue {
					message := fmt.Sprintf("Node %s is not yet ready because %s: %s", node.Name, cond.Reason, cond.Message)
					log.Infof(message)
					messages = append(messages, message)
					nodesReady = false
				}
			}
		}
	}

	message := nodesReadyMessage
	if len(messages) > 0 {
		message = strings.Join(messages, " ")
	}

	return nodesReady, message, nil
}
