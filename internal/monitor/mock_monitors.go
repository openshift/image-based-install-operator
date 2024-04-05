package monitor

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func SuccessMonitor(_ context.Context, _ logrus.FieldLogger, _ client.Client) (bool, error) {
	return true, nil
}

var _ GetInstallStatusFunc = SuccessMonitor

func FailureMonitor(_ context.Context, _ logrus.FieldLogger, _ client.Client) (bool, error) {
	return false, nil
}

var _ GetInstallStatusFunc = FailureMonitor

func ErrorMonitor(_ context.Context, _ logrus.FieldLogger, _ client.Client) (bool, error) {
	return false, fmt.Errorf("monitoring failed")
}

var _ GetInstallStatusFunc = ErrorMonitor
