package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/kelseyhightower/envconfig"
	"github.com/sirupsen/logrus"
)

var Options struct {
	ServerDir     string `envconfig:"SERVER_DIR" default:"/data/server"`
	Port          string `envconfig:"PORT" default:"8080"`
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

	if err := os.MkdirAll(Options.ServerDir, 0700); err != nil {
		log.Fatalf("Failed to create server dir: %s", err)
	}

	http.Handle("/images/", http.StripPrefix("/images/", http.FileServer(http.Dir(Options.ServerDir))))
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
		log.WithError(err).Errorf("shutdown failed")
		if err := server.Close(); err != nil {
			log.WithError(err).Fatal("emergency shutdown failed")
		}
	} else {
		log.Infof("server terminated gracefully")
	}
}
