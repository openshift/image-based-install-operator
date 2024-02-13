package credentials

import (
	"context"
	"crypto/rand"
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

	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	lca_api "github.com/openshift-kni/lifecycle-agent/api/seedreconfig"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/openshift/image-based-install-operator/internal/certs"
	"github.com/sirupsen/logrus"
)

const (
	DefaultUser    = "kubeadmin"
	kubeconfig     = "kubeconfig"
	kubeadmincreds = "kubeadmincreds"
)

type Credentials struct {
	client.Client
	Certs  certs.KubeConfigCertManager
	Log    logrus.FieldLogger
	Scheme *runtime.Scheme
}

func (r *Credentials) EnsureKubeconfigSecret(ctx context.Context, cd *hivev1.ClusterDeployment, clusterInfo *lca_api.SeedReconfiguration) (lca_api.KubeConfigCryptoRetention, error) {
	existsAndValid, err := r.kubeconfigExistsAndValid(ctx, cd, clusterInfo)
	if err != nil {
		return lca_api.KubeConfigCryptoRetention{}, err
	}
	if existsAndValid {
		return clusterInfo.KubeconfigCryptoRetention, nil
	}
	// Generate cluster certs and create a new kubeconfig.
	// TODO: handle user provided API and ingress certs
	r.Log.Infof("Generating cluster crypto")
	if err := r.Certs.GenerateAllCertificates(); err != nil {
		return lca_api.KubeConfigCryptoRetention{}, fmt.Errorf("failed to generate certificates: %w", err)
	}
	kubeconfigBytes, err := generateKubeConfig(fmt.Sprintf("%s.%s", cd.Spec.ClusterName, cd.Spec.BaseDomain),
		r.Certs.GetCertificateAuthData(),
		r.Certs.GetClientCert(),
		r.Certs.GetClientKey())
	if err != nil {
		return lca_api.KubeConfigCryptoRetention{}, fmt.Errorf("failed to generate kubeconfig: %w", err)
	}

	data := map[string][]byte{
		"kubeconfig": kubeconfigBytes,
	}

	if err := r.createOrUpdateClusterCredentialSecret(ctx, cd, KubeconfigSecretName(cd.Name), data, kubeconfig); err != nil {
		return lca_api.KubeConfigCryptoRetention{}, fmt.Errorf("failed to create kubeadmin password secret: %w", err)
	}
	return r.Certs.GetCrypto(), nil
}

func (r *Credentials) EnsureAdminPasswordSecret(ctx context.Context, cd *hivev1.ClusterDeployment) (string, error) {
	secretRef := types.NamespacedName{Namespace: cd.Namespace, Name: KubeadminPasswordSecretName(cd.Name)}
	secret := &corev1.Secret{}
	err := r.Get(ctx, secretRef, secret)
	if err == nil {
		password, exists := secret.Data["password"]
		if !exists {
			r.Log.Warn("failed to find password in secret, generating new one")
		} else {
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
		BlockOwnerDeletion: pointer.Bool(true),
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

func (r *Credentials) kubeconfigExistsAndValid(ctx context.Context, cd *hivev1.ClusterDeployment, clusterInfo *lca_api.SeedReconfiguration) (bool, error) {
	if clusterInfo == nil || clusterInfo.ClusterName != cd.Spec.ClusterName || clusterInfo.BaseDomain != cd.Spec.BaseDomain {
		return false, nil
	}
	secretRef := types.NamespacedName{Namespace: cd.Namespace, Name: KubeconfigSecretName(cd.Name)}
	secret := &corev1.Secret{}
	err := r.Get(ctx, secretRef, secret)
	if err == nil {
		_, exists := secret.Data["kubeconfig"]
		if !exists {
			r.Log.Warn("failed to find kubeconfig in secret, generating new one")
			return false, nil
		}
		// no changes to the cluster info, no error and the secret data have what we want.
		return true, nil
	}
	if !errors.IsNotFound(err) {
		return false, fmt.Errorf("failed to get kubeconfig secret: %w", err)
	}
	return false, nil
}

func KubeconfigSecretName(clusterDeploymentName string) string {
	return clusterDeploymentName + "-admin-kubeconfig"
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
			Server:                   fmt.Sprintf("https://api.%s:6443", url),
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
