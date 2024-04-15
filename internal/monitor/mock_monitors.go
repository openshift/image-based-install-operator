package monitor

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func SuccessMonitor(_ context.Context, _ logrus.FieldLogger, _ client.Client) (ClusterInstallStatus, error) {
	return ClusterInstallStatus{
		Installed:            true,
		ClusterVersionStatus: "ClusterVersion is available",
		NodesStatus:          "All nodes are ready",
	}, nil
}

var _ GetInstallStatusFunc = SuccessMonitor

func FailureMonitor(_ context.Context, _ logrus.FieldLogger, _ client.Client) (ClusterInstallStatus, error) {
	return ClusterInstallStatus{
		Installed:            false,
		ClusterVersionStatus: "Cluster version is not available",
		NodesStatus:          "Node test is NotReady",
	}, nil
}

var _ GetInstallStatusFunc = FailureMonitor

func ErrorMonitor(_ context.Context, _ logrus.FieldLogger, _ client.Client) (ClusterInstallStatus, error) {
	return ClusterInstallStatus{}, fmt.Errorf("monitoring failed")
}

var _ GetInstallStatusFunc = ErrorMonitor
