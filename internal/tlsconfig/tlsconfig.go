package tlsconfig

import (
	"context"
	"crypto/tls"
	"fmt"

	configv1 "github.com/openshift/api/config/v1"
	crtls "github.com/openshift/controller-runtime-common/pkg/tls"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TLSConfigResult holds the resolved TLS configuration
type TLSConfigResult struct {
	TLSConfig          func(*tls.Config)
	TLSAdherencePolicy configv1.TLSAdherencePolicy
	TLSProfileSpec     configv1.TLSProfileSpec
}

var log = ctrl.Log.WithName("tlsconfig")

// ResolveTLSConfig loads TLS settings from the APIServer, honoring spec.tlsAdherence and spec.tlsSecurityProfile
func ResolveTLSConfig(ctx context.Context, restConfig *rest.Config) (TLSConfigResult, error) {
	scheme := runtime.NewScheme()
	if err := configv1.AddToScheme(scheme); err != nil {
		return TLSConfigResult{}, fmt.Errorf("unable to add config.openshift.io/v1 to scheme: %w", err)
	}

	k8sClient, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		return TLSConfigResult{}, fmt.Errorf("unable to create Kubernetes client: %w", err)
	}

	tlsAdherencePolicy, err := crtls.FetchAPIServerTLSAdherencePolicy(ctx, k8sClient)
	if err != nil {
		log.Error(err, "unable to get TLS adherence policy from API server; defaulting until APIServer is readable")
		tlsAdherencePolicy = configv1.TLSAdherencePolicyNoOpinion
	}

	tlsProfileSpec, err := crtls.FetchAPIServerTLSProfile(ctx, k8sClient)
	if err != nil {
		log.Error(err, "unable to get TLS profile from API server; defaulting until APIServer is readable")
		tlsProfileSpec = *configv1.TLSProfiles[libgocrypto.DefaultTLSProfileType]
	}

	appliedProfile := *configv1.TLSProfiles[libgocrypto.DefaultTLSProfileType]
	if libgocrypto.ShouldHonorClusterTLSProfile(tlsAdherencePolicy) {
		appliedProfile = tlsProfileSpec
	}
	profileTLSConfig, unsupportedCiphers := crtls.NewTLSConfigFromProfile(appliedProfile)
	if len(unsupportedCiphers) > 0 {
		log.Info("TLS configuration contains unsupported ciphers that will be ignored", "unsupportedCiphers", unsupportedCiphers)
	}
	tlsConfig := profileTLSConfig

	return TLSConfigResult{
		TLSConfig:          tlsConfig,
		TLSAdherencePolicy: tlsAdherencePolicy,
		TLSProfileSpec:     tlsProfileSpec,
	}, nil
}
