package monitor

import (
	"context"

	"github.com/sirupsen/logrus"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func SuccessMonitor(_ context.Context, _ logrus.FieldLogger, _ client.Client) ClusterInstallStatus {
	return ClusterInstallStatus{
		Installed:            true,
		ClusterVersionStatus: "ClusterVersion is available",
		NodesStatus:          "All nodes are ready",
	}
}

var _ GetInstallStatusFunc = SuccessMonitor

func FailureMonitor(_ context.Context, _ logrus.FieldLogger, _ client.Client) ClusterInstallStatus {
	return ClusterInstallStatus{
		Installed:            false,
		ClusterVersionStatus: "Cluster version is not available",
		NodesStatus:          "Node test is NotReady",
	}
}

var _ GetInstallStatusFunc = FailureMonitor
