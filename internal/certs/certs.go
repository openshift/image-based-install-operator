package certs

import (
	"crypto/x509/pkix"
	"fmt"
	"time"

	"k8s.io/apiserver/pkg/authentication/user"

	lca_api "github.com/openshift-kni/lifecycle-agent/api/seedreconfig"
	"github.com/openshift/library-go/pkg/crypto"
)

type KubeConfigCertManager struct {
	crypto              lca_api.KubeConfigCryptoRetention
	certificateAuthData []byte
	userClientCert      []byte
	userClientKey       []byte
}

type CertInfo struct {
	commonName string
	validity   int
}

const (
	// validityTwoYearsInDays sets the validity of a cert to 2 years.
	validityTwoYearsInDays = 365 * 2

	// validityTenYearsInDays sets the validity of a cert to 10 years.
	validityTenYearsInDays = 365 * 10
)

func (r *KubeConfigCertManager) GenerateAllCertificates() error {
	r.dumpExistingCertificates()
	err := r.GenerateKubeApiserverServingSigningCerts()
	if err != nil {
		return fmt.Errorf("failed to generate the kube apiserver serving signing certificates: %w", err)
	}
	err = r.generateIngressServingSigningCerts()
	if err != nil {
		return fmt.Errorf("failed to generate the ingress signer certificates: %w", err)
	}

	adminKubeconfigSigner, err := generateSelfSignedCACertificate("admin-kubeconfig-signer", validityTenYearsInDays)
	if err != nil {
		return fmt.Errorf("failed to generate admin kubeconfig signer CA: %w", err)
	}
	certBytes, err := crypto.EncodeCertificates(adminKubeconfigSigner.Config.Certs...)
	if err != nil {
		return fmt.Errorf("failed to encode admin kubeconfig signer CA: %w", err)
	}
	r.crypto.KubeAPICrypto.ClientAuthCrypto.AdminCACertificate = lca_api.PEM(certBytes)

	r.userClientCert, r.userClientKey, err = generateAdminUserCertificate(adminKubeconfigSigner)
	if err != nil {
		return fmt.Errorf("failed to generate admin user certificate: %w", err)

	}
	return nil
}

// GenerateKubeApiserverServingSigningCerts Create the kapi serving signer CAs and adds them to the cluster CA bundle
func (r *KubeConfigCertManager) GenerateKubeApiserverServingSigningCerts() error {
	certBytes, keyBytes, err := r.generateServingSigningCerts("kube-apiserver-lb-signer", validityTenYearsInDays)
	if err != nil {
		return err
	}
	// Append the PEM-encoded certificate to the cluster CA bundle
	r.certificateAuthData = append(r.certificateAuthData, certBytes...)
	// Save the private key to be added to cluster config
	r.crypto.KubeAPICrypto.ServingCrypto.LoadbalancerSignerPrivateKey = lca_api.PEM(keyBytes)

	certBytes, keyBytes, err = r.generateServingSigningCerts("kube-apiserver-localhost-signer", validityTenYearsInDays)
	if err != nil {
		return err
	}
	// Append the PEM-encoded certificate to the cluster CA bundle
	r.certificateAuthData = append(r.certificateAuthData, certBytes...)
	// Save the private key to be added to cluster config
	r.crypto.KubeAPICrypto.ServingCrypto.LocalhostSignerPrivateKey = lca_api.PEM(keyBytes)

	certBytes, keyBytes, err = r.generateServingSigningCerts("kube-apiserver-service-network-signer", validityTenYearsInDays)
	if err != nil {
		return err
	}
	// Append the PEM-encoded certificate to the cluster CA bundle
	r.certificateAuthData = append(r.certificateAuthData, certBytes...)
	// Save the private key to be added to cluster config
	r.crypto.KubeAPICrypto.ServingCrypto.ServiceNetworkSignerPrivateKey = lca_api.PEM(keyBytes)

	return nil
}

func (r *KubeConfigCertManager) GetCrypto() lca_api.KubeConfigCryptoRetention {
	return r.crypto
}

func (r *KubeConfigCertManager) GetCertificateAuthData() []byte {
	return r.certificateAuthData
}

func (r *KubeConfigCertManager) GetClientCert() []byte {
	return r.userClientCert
}

func (r *KubeConfigCertManager) GetClientKey() []byte {
	return r.userClientKey
}

// GenerateIngressServingSigningCerts Create the ingress serving signer CAs and adds them to the cluster CA bundle
func (r *KubeConfigCertManager) generateIngressServingSigningCerts() error {
	certBytes, keyBytes, err := r.generateServingSigningCerts(
		fmt.Sprintf("%s@%d", "ingress-operator", time.Now().Unix()),
		validityTwoYearsInDays)
	if err != nil {
		return err
	}
	// Append the PEM-encoded certificate to the cluster CA bundle
	r.certificateAuthData = append(r.certificateAuthData, certBytes...)
	// Append the PEM-encoded certificate to the cluster CA bundle
	r.crypto.IngresssCrypto.IngressCA = lca_api.PEM(keyBytes)
	return nil
}

// GenerateServingSigningCerts Creates a serving signer CAs and returns the key and cert
func (r *KubeConfigCertManager) generateServingSigningCerts(commonName string, validity int) ([]byte, []byte, error) {
	ca, err := generateSelfSignedCACertificate(commonName, validity)
	if err != nil {
		return nil, nil, err
	}
	return ca.Config.GetPEMBytes()
}

func (r *KubeConfigCertManager) dumpExistingCertificates() {
	r.certificateAuthData = nil
	r.userClientCert = nil
	r.userClientKey = nil
	r.crypto = lca_api.KubeConfigCryptoRetention{}
}

func generateSelfSignedCACertificate(commonName string, validity int) (*crypto.CA, error) {
	subject := pkix.Name{CommonName: commonName, OrganizationalUnit: []string{"openshift"}}
	newCAConfig, err := crypto.MakeSelfSignedCAConfigForSubject(
		subject,
		validity,
	)
	if err != nil {
		return nil, fmt.Errorf("error generating self signed CA: %w", err)
	}
	return &crypto.CA{
		SerialGenerator: &crypto.RandomSerialGenerator{},
		Config:          newCAConfig,
	}, nil
}

func generateAdminUserCertificate(ca *crypto.CA) ([]byte, []byte, error) {
	user := user.DefaultInfo{Name: "system:admin"}
	lifetime := validityTenYearsInDays * 24 * time.Hour

	cfg, err := ca.MakeClientCertificateForDuration(&user, lifetime)
	if err != nil {
		return nil, nil, fmt.Errorf("error making client certificate: %w", err)
	}
	crt, key, err := cfg.GetPEMBytes()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get PEM bytes for system:admin client certificate: %w", err)
	}

	return crt, key, nil
}
