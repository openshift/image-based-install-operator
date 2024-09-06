package monitor

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/sirupsen/logrus"
)

const (
	nodesReadyMessage              = "All nodes are ready"
	clusterVersionAvailableMessage = "ClusterVersion is available"
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

func GetClusterInstallStatus(ctx context.Context, log logrus.FieldLogger, c client.Client) ClusterInstallStatus {
	cvAvailable, cvMessage, err := clusterVersionStatus(ctx, log, c)
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

func clusterVersionStatus(ctx context.Context, log logrus.FieldLogger, c client.Client) (bool, string, error) {
	cv := &configv1.ClusterVersion{}
	if err := c.Get(ctx, types.NamespacedName{Name: "version"}, cv); err != nil {
		return false, "", err
	}

	for _, cond := range cv.Status.Conditions {
		if cond.Type == configv1.OperatorAvailable {
			if cond.Status == configv1.ConditionTrue {
				return true, clusterVersionAvailableMessage, nil
			} else {
				message := fmt.Sprintf("ClusterVersion is not yet available because %s: %s", cond.Reason, cond.Message)
				log.Infof(message)
				return false, message, nil
			}
		}
	}

	return false, "ClusterVersion Available condition not found", nil
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
