package monitor

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/sirupsen/logrus"
)

type GetInstallStatusFunc func(ctx context.Context, log logrus.FieldLogger, c client.Client) (bool, error)

func IsClusterInstalled(ctx context.Context, log logrus.FieldLogger, c client.Client) (bool, error) {
	cvAvailable, err := isClusterVersionAvailable(ctx, log, c)
	if err != nil {
		return false, fmt.Errorf("failed to check cluster version status: %w", err)
	}
	if !cvAvailable {
		return false, nil
	}

	nodesReady, err := areNodesReady(ctx, log, c)
	if err != nil {
		return false, fmt.Errorf("failed to check node status: %w", err)
	}

	return nodesReady, nil
}

func isClusterVersionAvailable(ctx context.Context, log logrus.FieldLogger, c client.Client) (bool, error) {
	cv := &configv1.ClusterVersion{}
	if err := c.Get(ctx, types.NamespacedName{Name: "version"}, cv); err != nil {
		return false, err
	}

	isAvailable := false
	for _, cond := range cv.Status.Conditions {
		if cond.Type == configv1.OperatorAvailable {
			if cond.Status == configv1.ConditionTrue {
				isAvailable = true
				break
			} else {
				log.Infof("cluster version is not yet available because %s: %s", cond.Reason, cond.Message)
			}
		}
	}

	return isAvailable, nil
}

func areNodesReady(ctx context.Context, log logrus.FieldLogger, c client.Client) (bool, error) {
	nodes := &corev1.NodeList{}
	if err := c.List(ctx, nodes); err != nil {
		return false, err
	}
	if len(nodes.Items) == 0 {
		log.Info("no nodes found")
		return false, nil
	}

	nodesReady := true
	for _, node := range nodes.Items {
		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeReady {
				if cond.Status != corev1.ConditionTrue {
					log.Infof("node %s is not yet ready", node.Name)
					nodesReady = false
				}
			}
		}
	}

	return nodesReady, nil
}
