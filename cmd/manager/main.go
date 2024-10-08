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

package main

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/kelseyhightower/envconfig"
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/openshift/image-based-install-operator/api/v1alpha1"
	"github.com/openshift/image-based-install-operator/controllers"
	"github.com/openshift/image-based-install-operator/internal/credentials"
	"github.com/openshift/image-based-install-operator/internal/installer"
	"github.com/openshift/image-based-install-operator/internal/monitor"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	utilruntime.Must(bmh_v1alpha1.AddToScheme(scheme))
	utilruntime.Must(hivev1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "e21b2704.openshift.io",
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:    9443,
			CertDir: "/webhook-certs",
		}),
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	logger := logrus.New()
	logger.SetReportCaller(true)

	controllerOptions := &controllers.ImageClusterInstallReconcilerOptions{}
	if err := envconfig.Process("image-based-install-operator", controllerOptions); err != nil {
		setupLog.Error(err, "unable to process envconfig")
		os.Exit(1)
	}
	credentialsManager := credentials.Credentials{
		Client: mgr.GetClient(),
		Log:    logger,
		Scheme: mgr.GetScheme(),
	}

	baseURL, err := serviceURL(controllerOptions)
	if err != nil {
		setupLog.Error(err, "failed to determine route base URL")
		os.Exit(1)
	}

	if err = (&controllers.ImageClusterInstallReconciler{
		Client:                       mgr.GetClient(),
		Credentials:                  credentialsManager,
		Log:                          logger,
		Scheme:                       mgr.GetScheme(),
		Options:                      controllerOptions,
		BaseURL:                      baseURL,
		DefaultInstallTimeout:        time.Hour,
		GetSpokeClusterInstallStatus: monitor.GetClusterInstallStatus,
		Installer:                    installer.NewInstaller(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ImageClusterInstall")
		os.Exit(1)
	}
	if err = (&v1alpha1.ImageClusterInstall{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "ImageClusterInstall")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func serviceURL(opts *controllers.ImageClusterInstallReconcilerOptions) (string, error) {
	if opts.ServiceName == "" || opts.ServiceNamespace == "" || opts.ServiceScheme == "" {
		return "", fmt.Errorf("SERVICE_NAME, SERVICE_NAMESPACE, and SERVICE_SCHEME must be set")
	}

	host := fmt.Sprintf("%s.%s.svc", opts.ServiceName, opts.ServiceNamespace)
	if opts.ServicePort != "" {
		host = fmt.Sprintf("%s:%s", host, opts.ServicePort)
	}

	return (&url.URL{Scheme: opts.ServiceScheme, Host: host}).String(), nil
}
