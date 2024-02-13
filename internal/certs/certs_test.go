package certs

import (
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"k8s.io/client-go/util/cert"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("KubeConfigCertManager", func() {
	var (
		cm KubeConfigCertManager
	)
	BeforeEach(func() {
		var err error
		Expect(err).NotTo(HaveOccurred())
		cm = KubeConfigCertManager{}
		cm.dumpExistingCertificates()
	})

	It("generateCA success", func() {
		commonName := "admin-kubeconfig-signer"
		ca, err := generateSelfSignedCACertificate(commonName, validityTenYearsInDays)
		Expect(err).NotTo(HaveOccurred())
		checkCertValidity(ca.Config.Certs[0], time.Duration(validityTenYearsInDays)*24*time.Hour)
	})
	It("GenerateKubeApiserverServingSigningCerts success", func() {
		err := cm.GenerateKubeApiserverServingSigningCerts()
		Expect(err).NotTo(HaveOccurred())
		// verify all apiserver signer keys exists
		Expect(string(cm.GetCrypto().KubeAPICrypto.ServingCrypto.LoadbalancerSignerPrivateKey)).Should(ContainSubstring("BEGIN RSA PRIVATE KEY"))
		Expect(string(cm.GetCrypto().KubeAPICrypto.ServingCrypto.LocalhostSignerPrivateKey)).Should(ContainSubstring("BEGIN RSA PRIVATE KEY"))
		Expect(string(cm.GetCrypto().KubeAPICrypto.ServingCrypto.ServiceNetworkSignerPrivateKey)).Should(ContainSubstring("BEGIN RSA PRIVATE KEY"))
		// verify All 3 api certs are in certificateAuthData
		Expect(cm.certificateAuthData).ToNot(BeNil())
		certs, err := cert.ParseCertsPEM(cm.certificateAuthData)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(certs)).To(Equal(3))
		for _, cert := range certs {
			Expect(cert.Subject.CommonName).Should(ContainSubstring("apiserver"))
			checkCertValidity(cert, time.Duration(validityTenYearsInDays)*24*time.Hour)
		}
	})
	It("GenerateIngressServingSigningCerts success", func() {
		err := cm.generateIngressServingSigningCerts()
		Expect(err).NotTo(HaveOccurred())
		// verify ingress CA exists
		Expect(string(cm.GetCrypto().IngresssCrypto.IngressCA)).Should(ContainSubstring("BEGIN RSA PRIVATE KEY"))

		block, _ := pem.Decode(cm.certificateAuthData)
		Expect(block).NotTo(Equal(nil))
		// Parse the CA certificate
		ingressCert, err := x509.ParseCertificate(block.Bytes)
		checkCertValidity(ingressCert, time.Duration(validityTwoYearsInDays)*24*time.Hour)
		// verify ingress cert in certificateAuthData
		Expect(cm.certificateAuthData).ToNot(BeNil())
		certs, err := cert.ParseCertsPEM(cm.certificateAuthData)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(certs)).To(Equal(1))
		Expect(certs[0].Subject.CommonName).Should(ContainSubstring("ingress-operator"))
	})

	It("generateAdminUserCertificate success", func() {
		ca, err := generateSelfSignedCACertificate("admin-kubeconfig-signer", validityTenYearsInDays)
		userCert, _, err := generateAdminUserCertificate(ca)
		Expect(err).NotTo(HaveOccurred())

		// Verify that the client cert was signed by the given
		block, _ := pem.Decode(userCert)
		cert, err := x509.ParseCertificate(block.Bytes)
		Expect(err).NotTo(HaveOccurred())
		cert.CheckSignatureFrom(ca.Config.Certs[0])
		checkCertValidity(cert, time.Duration(validityTenYearsInDays)*24*time.Hour)
	})
})

func checkCertValidity(cert *x509.Certificate, expectedValidity time.Duration) {
	currentTime := time.Now()
	// When creating the cert NotBefore is set to currentTime minus 1 second
	startDate := cert.NotBefore.Add(1 * time.Second)
	Expect(currentTime.Before(startDate)).To(BeFalse())
	oneMinuteFromNow := currentTime.Add(time.Minute)
	Expect(oneMinuteFromNow.After(startDate)).To(BeTrue())
	notAfter := startDate.Add(expectedValidity) // Valid for 10 years
	Expect(cert.NotAfter).To(Equal(notAfter))
}

func TestCertManager(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Certs Suite")
}
