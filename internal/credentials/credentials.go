package credentials

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"

	"golang.org/x/crypto/bcrypt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	lca_api "github.com/openshift-kni/lifecycle-agent/api/seedreconfig"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/openshift/image-based-install-operator/internal/certs"
	"github.com/sirupsen/logrus"
)

const (
	SecretResourceLabel = "image-based-installed.openshift.io/created"
	SecretResourceValue = "true"
	DefaultUser         = "kubeadmin"
	kubeconfig          = "kubeconfig"
	kubeadmincreds      = "kubeadmincreds"
	cryptoKeys          = "crypto-keys"
)

type CryptoSecret struct {
	CryptoKeys   lca_api.KubeConfigCryptoRetention `json:"keys,omitempty"`
	CertAuthData string                            `json:"cert_auth_data,omitempty"`
	ClientCert   string                            `json:"client_cert,omitempty"`
	ClientKey    string                            `json:"client_key,omitempty"`
}

type Credentials struct {
	client.Client
	Certs  certs.KubeConfigCertManager
	Log    logrus.FieldLogger
	Scheme *runtime.Scheme
}

func (r *Credentials) EnsureKubeconfigSecret(ctx context.Context, cd *hivev1.ClusterDeployment) (lca_api.KubeConfigCryptoRetention, error) {
	url := fmt.Sprintf("https://api.%s.%s:6443", cd.Spec.ClusterName, cd.Spec.BaseDomain)

	existsAndValid, err := r.kubeconfigExistsAndValid(ctx, cd, url)
	if err != nil {
		return lca_api.KubeConfigCryptoRetention{}, err
	}

	cryptoData, err := r.ensureCryptoKeys(ctx, cd, !existsAndValid)
	if err != nil {
		return lca_api.KubeConfigCryptoRetention{}, fmt.Errorf("failed to ensure crypto keys: %w", err)
	}
	if existsAndValid {
		r.Log.Debug("Kubeconfig already exists and is valid, taking crypto from secret")
		return cryptoData.CryptoKeys, nil
	}

	r.Log.Infof("Generating kubeconfig")
	kubeconfigBytes, err := generateKubeConfig(url,
		[]byte(cryptoData.CertAuthData), []byte(cryptoData.ClientCert), []byte(cryptoData.ClientKey))
	if err != nil {
		return lca_api.KubeConfigCryptoRetention{}, fmt.Errorf("failed to generate kubeconfig: %w", err)
	}

	data := map[string][]byte{
		"kubeconfig": kubeconfigBytes,
	}

	if err := r.createOrUpdateClusterCredentialSecret(ctx, cd, KubeconfigSecretName(cd.Name), data, kubeconfig); err != nil {
		return lca_api.KubeConfigCryptoRetention{}, fmt.Errorf("failed to create kubeadmin password secret: %w", err)
	}

	return cryptoData.CryptoKeys, nil
}

func (r *Credentials) ensureCryptoKeys(ctx context.Context, cd *hivev1.ClusterDeployment, forceRecreate bool) (CryptoSecret, error) {
	secret := &corev1.Secret{}
	var cryptoSecret CryptoSecret
	if !forceRecreate {
		if err := r.Client.Get(ctx, types.NamespacedName{Name: CryptoKeysSecretName(cd.Name), Namespace: cd.Namespace}, secret); err != nil && !errors.IsNotFound(err) {
			return cryptoSecret, fmt.Errorf("failed to get crypto keys secret")
		}

		cryptoData, ok := secret.Data[cryptoKeys]
		if ok {
			r.Log.Debug("Crypto keys already exist, taking them from the secret")
			err := json.Unmarshal(cryptoData, &cryptoSecret)
			if err != nil {
				return cryptoSecret, fmt.Errorf("failed to marshal crypto keys, err %w", err)
			}
			return cryptoSecret, nil
		}
	}

	r.Log.Infof("Generating cluster crypto")
	if err := r.Certs.GenerateAllCertificates(); err != nil {
		return cryptoSecret, fmt.Errorf("failed to generate certificates: %w", err)
	}

	cryptoSecret = CryptoSecret{
		CryptoKeys:   r.Certs.GetCrypto(),
		CertAuthData: string(r.Certs.GetCertificateAuthData()),
		ClientCert:   string(r.Certs.GetClientCert()),
		ClientKey:    string(r.Certs.GetClientKey()),
	}

	cryptoBytes, err := json.Marshal(cryptoSecret)
	if err != nil {
		return CryptoSecret{}, fmt.Errorf("failed to marshal crypto keys, err %w", err)
	}

	data := map[string][]byte{
		cryptoKeys: cryptoBytes,
	}

	if err := r.createOrUpdateClusterCredentialSecret(ctx, cd, CryptoKeysSecretName(cd.Name), data, cryptoKeys); err != nil {
		return CryptoSecret{}, fmt.Errorf("failed to create %s secret: %w", CryptoKeysSecretName(cd.Name), err)
	}
	return cryptoSecret, nil
}

func (r *Credentials) EnsureAdminPasswordSecret(ctx context.Context, cd *hivev1.ClusterDeployment, currentHash string) (string, error) {
	secretRef := types.NamespacedName{Namespace: cd.Namespace, Name: KubeadminPasswordSecretName(cd.Name)}
	secret := &corev1.Secret{}
	err := r.Get(ctx, secretRef, secret)
	if err == nil {
		password, exists := secret.Data["password"]
		if !exists {
			r.Log.Warn("failed to find password in secret, generating new one")
		} else {
			// in case password was not changed, return the existing hash
			if err := bcrypt.CompareHashAndPassword([]byte(currentHash), password); err == nil {
				r.Log.Debug("Admin password already exists and is valid")
				return currentHash, nil
			}
			// we will generate a new hash in case password was changed
			passwordHash, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
			if err != nil {
				return "", fmt.Errorf("failed to generate password hash: %w", err)
			}

			return string(passwordHash), nil
		}
	} else if !errors.IsNotFound(err) {
		return "", fmt.Errorf("failed to get kubeadmin password secret: %w", err)
	}
	r.Log.Infof("Generating admin password")
	password, err := generateKubeadminPassword()
	if err != nil {
		return "", fmt.Errorf("failed to generate password: %w", err)
	}
	data := map[string][]byte{
		"username": []byte(DefaultUser),
		"password": []byte(password),
	}

	if err := r.createOrUpdateClusterCredentialSecret(ctx, cd, KubeadminPasswordSecretName(cd.Name), data, kubeadmincreds); err != nil {
		return "", fmt.Errorf("failed to create kubeadmin password secret: %w", err)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("failed to generate password hash: %w", err)
	}
	return string(passwordHash), nil
}

func (r *Credentials) createOrUpdateClusterCredentialSecret(ctx context.Context, cd *hivev1.ClusterDeployment, name string, data map[string][]byte, secretType string) error {
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cd.Namespace,
		},
		Data: data,
		Type: corev1.SecretTypeOpaque,
	}

	secret.Labels = addLabel(secret.Labels, "hive.openshift.io/cluster-deployment-name", cd.Name)
	secret.Labels = addLabel(secret.Labels, "hive.openshift.io/secret-type", secretType)
	secret.Labels = addLabel(secret.Labels, SecretResourceLabel, SecretResourceValue)

	deploymentGVK, err := apiutil.GVKForObject(cd, r.Scheme)
	if err != nil {
		r.Log.WithError(err).Errorf("error getting GVK for clusterdeployment")
		return err
	}
	secret.OwnerReferences = []metav1.OwnerReference{{
		APIVersion:         deploymentGVK.GroupVersion().String(),
		Kind:               deploymentGVK.Kind,
		Name:               cd.Name,
		UID:                cd.UID,
		BlockOwnerDeletion: ptr.To(true),
	}}
	mutateFn := func() error {
		// Update the Secret object with the desired data
		secret.Data = data
		return nil
	}
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, mutateFn)
	if err != nil {
		return fmt.Errorf("failed to create secret: %w", err)
	}
	r.Log.Infof("%s secret %s", secretType, op)
	return nil
}

func (r *Credentials) kubeconfigExistsAndValid(ctx context.Context, cd *hivev1.ClusterDeployment, url string) (bool, error) {
	secretRef := types.NamespacedName{Namespace: cd.Namespace, Name: KubeconfigSecretName(cd.Name)}
	secret := &corev1.Secret{}
	err := r.Get(ctx, secretRef, secret)
	if err == nil {
		kubeconfigData, exists := secret.Data["kubeconfig"]
		if !exists {
			r.Log.Warn("failed to find kubeconfig in secret, generating new one")
			return false, nil
		}
		kubeCfg, err := clientcmd.Load(kubeconfigData)
		if err != nil {
			return false, fmt.Errorf("failed to load kubeconfig: %w", err)
		}
		cluster, ok := kubeCfg.Clusters["cluster"]
		return ok && cluster.Server == url, nil

	}
	if !errors.IsNotFound(err) {
		return false, fmt.Errorf("failed to get kubeconfig secret: %w", err)
	}
	return false, nil
}

func KubeconfigSecretName(clusterDeploymentName string) string {
	return clusterDeploymentName + "-admin-kubeconfig"
}

func CryptoKeysSecretName(clusterDeploymentName string) string {
	return clusterDeploymentName + "-crypto-keys"
}

func KubeadminPasswordSecretName(clusterDeploymentName string) string {
	return clusterDeploymentName + "-admin-password"
}

func generateKubeConfig(url string, certificateAuthData, userClientCert, userClientKey []byte) ([]byte, error) {
	kubeCfg := clientcmdapi.Config{
		Kind:       "Config",
		APIVersion: "v1",
	}
	kubeCfg.Clusters = map[string]*clientcmdapi.Cluster{
		"cluster": {
			Server:                   url,
			CertificateAuthorityData: certificateAuthData,
		},
	}
	kubeCfg.AuthInfos = map[string]*clientcmdapi.AuthInfo{
		"admin": {
			ClientCertificateData: userClientCert,
			ClientKeyData:         userClientKey,
		},
	}
	kubeCfg.Contexts = map[string]*clientcmdapi.Context{
		"admin": {
			Cluster:   "cluster",
			AuthInfo:  "admin",
			Namespace: "default",
		},
	}
	kubeCfg.CurrentContext = "admin"
	return clientcmd.Write(kubeCfg)
}

func generateKubeadminPassword() (string, error) {
	// This code was copied from the openshift installer
	const (
		lowerLetters = "abcdefghijkmnopqrstuvwxyz"
		upperLetters = "ABCDEFGHIJKLMNPQRSTUVWXYZ"
		digits       = "23456789"
		all          = lowerLetters + upperLetters + digits
		length       = 23
	)
	var password string
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(all))))
		if err != nil {
			return "", err
		}
		newchar := string(all[n.Int64()])
		if password == "" {
			password = newchar
		}
		if i < length-1 {
			n, err = rand.Int(rand.Reader, big.NewInt(int64(len(password)+1)))
			if err != nil {
				return "", err
			}
			j := n.Int64()
			password = password[0:j] + newchar + password[j:]
		}
	}
	pw := []rune(password)
	for _, replace := range []int{5, 11, 17} {
		pw[replace] = '-'
	}
	return string(pw), nil
}

func addLabel(labels map[string]string, labelKey, labelValue string) map[string]string {
	if labelKey == "" {
		// Don't need to add a label.
		return labels
	}
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[labelKey] = labelValue
	return labels
}
