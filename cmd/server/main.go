package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/cluster-relocation-service/internal/imageserver"
	"github.com/sirupsen/logrus"
)

var Options struct {
	DataDir       string `envconfig:"DATA_DIR" default:"/data"`
	Port          string `envconfig:"PORT" default:"8000"`
	HTTPSKeyFile  string `envconfig:"HTTPS_KEY_FILE"`
	HTTPSCertFile string `envconfig:"HTTPS_CERT_FILE"`
}

func main() {
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
		Addr: fmt.Sprintf(":%s", Options.Port),
	}

	go func() {
		var err error
		if Options.HTTPSKeyFile != "" && Options.HTTPSCertFile != "" {
			log.Infof("Starting https handler on %s...", server.Addr)
			err = server.ListenAndServeTLS(Options.HTTPSCertFile, Options.HTTPSKeyFile)
		} else {
			log.Infof("Starting http handler on %s...", server.Addr)
			err = server.ListenAndServe()
		}

		if err != http.ErrServerClosed {
			log.WithError(err).Fatalf("HTTP listener closed: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	if err := server.Shutdown(context.Background()); err != nil {
		log.WithError(err).Error("shutdown failed")
		if err := server.Close(); err != nil {
			log.WithError(err).Fatal("emergency shutdown failed")
		}
	} else {
		log.Info("server terminated gracefully")
	}
}
