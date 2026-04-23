package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kelseyhightower/envconfig"
	configv1 "github.com/openshift/api/config/v1"
	configclientset "github.com/openshift/client-go/config/clientset/versioned"
	crtls "github.com/openshift/controller-runtime-common/pkg/tls"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/openshift/image-based-install-operator/internal/imageserver"
	"github.com/openshift/image-based-install-operator/internal/tlsconfig"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var Options struct {
	DataDir       string `envconfig:"DATA_DIR" default:"/data"`
	Port          string `envconfig:"PORT" default:"8000"`
	HTTPSKeyFile  string `envconfig:"HTTPS_KEY_FILE"`
	HTTPSCertFile string `envconfig:"HTTPS_CERT_FILE"`
}

func main() {
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := logrus.New()
	log.SetReportCaller(true)

	err := envconfig.Process("fileserver", &Options)
	if err != nil {
		log.Fatalf("Failed to process config: %s", err)
	}

	workDir := filepath.Join(Options.DataDir, "iso-workdir")
	if err := os.MkdirAll(workDir, 0700); err != nil {
		log.Fatalf("Failed to create work dir: %s", err)
	}

	s := &imageserver.Handler{
		Log:        log,
		WorkDir:    workDir,
		ConfigsDir: filepath.Join(Options.DataDir, "namespaces"),
	}
	http.Handle("/images/", s)
	server := &http.Server{
		Addr:              fmt.Sprintf(":%s", Options.Port),
		ReadHeaderTimeout: 5 * time.Second,
	}

	watchCtx, watchCancel := context.WithCancel(context.Background())
	defer watchCancel()
	tlsProfileChanged := make(chan struct{}, 1)

	go func() {
		var err error
		if Options.HTTPSKeyFile != "" && Options.HTTPSCertFile != "" {
			var tlsResult tlsconfig.TLSConfigResult
			log.Infof("Starting https handler on %s...", server.Addr)

			restCfg := ctrl.GetConfigOrDie()
			tlsResult, err = tlsconfig.ResolveTLSConfig(context.Background(), restCfg)
			if err != nil {
				log.WithError(err).Fatal("unable to configure HTTPS TLS")
			}
			go watchAndExitOnTLSChange(watchCtx, log, configclientset.NewForConfigOrDie(restCfg), tlsResult, tlsProfileChanged)

			if tlsResult.TLSConfig != nil {
				server.TLSConfig = &tls.Config{}
				tlsResult.TLSConfig(server.TLSConfig)
			}
			err = server.ListenAndServeTLS(Options.HTTPSCertFile, Options.HTTPSKeyFile)
		} else {
			log.Infof("Starting http handler on %s...", server.Addr)
			err = server.ListenAndServe()
		}

		if err != http.ErrServerClosed {
			log.WithError(err).Fatalf("HTTP listener closed: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case <-sigCh:
	case <-tlsProfileChanged:
		log.Info("initiating graceful shutdown after cluster TLS configuration change")
	}
	watchCancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.WithError(err).Error("shutdown failed")
		if err := server.Close(); err != nil {
			log.WithError(err).Fatal("emergency shutdown failed")
		}
	} else {
		log.Info("server terminated gracefully")
	}
}

func watchAndExitOnTLSChange(ctx context.Context, log *logrus.Logger, configClient configclientset.Interface, current tlsconfig.TLSConfigResult, requestShutdown chan<- struct{}) {
	w, err := configClient.ConfigV1().APIServers().Watch(ctx, metav1.ListOptions{
		FieldSelector: "metadata.name=cluster",
	})
	if err != nil {
		log.WithError(err).Error("failed to watch the APIServer, TLS updates will not be monitored")
		return
	}
	defer w.Stop()

	for event := range w.ResultChan() {
		if event.Type != watch.Modified {
			continue
		}
		updated, ok := event.Object.(*configv1.APIServer)
		if !ok {
			continue
		}

		if current.TLSAdherencePolicy != updated.Spec.TLSAdherence {
			log.Infof("TLS adherence policy has changed, shutting down to reload, oldPolicy: %v, newPolicy: %v",
				current.TLSAdherencePolicy, updated.Spec.TLSAdherence)
			requestGracefulShutdown(requestShutdown)
			return
		}

		profile, err := crtls.GetTLSProfileSpec(updated.Spec.TLSSecurityProfile)
		if err != nil {
			log.WithError(err).Error("failed to load TLS profile spec after APIServer update, ignoring")
			continue
		}
		if !equality.Semantic.DeepEqual(current.TLSProfileSpec, profile) {
			log.Infof("TLS profile has changed, shutting down to reload, oldProfile: %v, newProfile: %v",
				current.TLSProfileSpec, profile)
			requestGracefulShutdown(requestShutdown)
			return
		}
	}

	if errors.Is(ctx.Err(), context.Canceled) {
		log.Info("stopped monitoring APIServer TLS updates")
		return
	}
	log.Error("watch on APIServer exited, TLS updates will not be monitored")
}

func requestGracefulShutdown(ch chan<- struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
