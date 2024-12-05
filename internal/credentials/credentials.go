package credentials

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/openshift/installer/pkg/types/imagebased"
	"github.com/sirupsen/logrus"
)

const (
	SecretResourceLabel         = "image-based-installed.openshift.io/created"
	SecretResourceValue         = "true"
	DefaultUser                 = "kubeadmin"
	Kubeconfig                  = "kubeconfig"
	kubeadmincreds              = "kubeadmincreds"
	kubeAdminKey                = "password"
	SeedReconfigurationFileName = "manifest.json"
)

//go:generate mockgen --build_flags=--mod=mod -package=credentials -destination=mock_client.go sigs.k8s.io/controller-runtime/pkg/client Client

type Credentials struct {
	client.Client
	Log    logrus.FieldLogger
	Scheme *runtime.Scheme
}

func (r *Credentials) secretExistsAndValid(ctx context.Context, log logrus.FieldLogger, secretRef types.NamespacedName, key string, data []byte) (bool, error) {
	secret := &corev1.Secret{}
	err := r.Get(ctx, secretRef, secret)
	if errors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get kubeconfig secret: %w", err)
	}

	secretData, exists := secret.Data[key]
	if !exists {
		log.Warn("failed to find kubeconfig in secret, generating new one")
		return false, nil
	}
	if bytes.Equal(secretData, data) {
		log.Debugf("secret %s already exists and valid", secretRef.Name)
		return true, nil
	}

	return false, nil
}

func (r *Credentials) EnsureKubeconfigSecret(ctx context.Context,
	log logrus.FieldLogger,
	cd *hivev1.ClusterDeployment,
	kubeconfigFile string) error {

	kubeconfigData, err := os.ReadFile(kubeconfigFile)
	if err != nil {
		return fmt.Errorf("failed to read kubeadmin file %s: %w", kubeconfigFile, err)
	}

	secretRef := types.NamespacedName{Namespace: cd.Namespace, Name: KubeconfigSecretName(cd.Name)}
	if exists, err := r.secretExistsAndValid(ctx, log, secretRef, Kubeconfig, kubeconfigData); err != nil || exists {
		return err
	}

	data := map[string][]byte{
		"kubeconfig": kubeconfigData,
	}

	if err := r.createOrUpdateClusterCredentialSecret(ctx, log, cd, KubeconfigSecretName(cd.Name), data, Kubeconfig); err != nil {
		return fmt.Errorf("failed to create kubeadmin password secret: %w", err)
	}

	return nil
}

func (r *Credentials) EnsureAdminPasswordSecret(ctx context.Context,
	log logrus.FieldLogger,
	cd *hivev1.ClusterDeployment, kubeAdminFile string) error {
	password, err := os.ReadFile(kubeAdminFile)
	if err != nil {
		return fmt.Errorf("failed to read kubeadmin file %s: %w", kubeAdminFile, err)
	}

	secretRef := types.NamespacedName{Namespace: cd.Namespace, Name: KubeadminPasswordSecretName(cd.Name)}
	if exists, err := r.secretExistsAndValid(ctx, log, secretRef, kubeAdminKey, password); err != nil || exists {
		return err
	}

	data := map[string][]byte{
		"username":   []byte(DefaultUser),
		kubeAdminKey: password,
	}

	if err := r.createOrUpdateClusterCredentialSecret(ctx, log, cd, KubeadminPasswordSecretName(cd.Name), data, kubeadmincreds); err != nil {
		return fmt.Errorf("failed to create kubeadmin password secret: %w", err)
	}
	return nil
}

func (r *Credentials) EnsureSeedReconfigurationSecret(ctx context.Context,
	log logrus.FieldLogger,
	cd *hivev1.ClusterDeployment,
	seedReconfigurationFile string) error {

	seedReconfigurationData, err := os.ReadFile(seedReconfigurationFile)
	if err != nil {
		return fmt.Errorf("failed to read seedReconfigurationFile file %s: %w", seedReconfigurationFile, err)
	}

	secretRef := types.NamespacedName{Namespace: cd.Namespace, Name: SeedReconfigurationSecretName(cd.Name)}
	if exists, err := r.secretExistsAndValid(ctx, log, secretRef, SeedReconfigurationFileName, seedReconfigurationData); err != nil || exists {
		return err
	}

	data := map[string][]byte{
		SeedReconfigurationFileName: seedReconfigurationData,
	}

	if err := r.createOrUpdateClusterCredentialSecret(ctx, log, cd, SeedReconfigurationSecretName(cd.Name), data, SeedReconfigurationFileName); err != nil {
		return fmt.Errorf("failed to create kubeadmin password secret: %w", err)
	}

	return nil
}

func (r *Credentials) SeedReconfigSecretClusterIDs(ctx context.Context, log logrus.FieldLogger, cd *hivev1.ClusterDeployment) (string, string, error) {
	secretRef := types.NamespacedName{Namespace: cd.Namespace, Name: SeedReconfigurationSecretName(cd.Name)}
	secretSeedReconfigurationData, present, err := r.getSecretContent(ctx, secretRef, SeedReconfigurationFileName)
	if !present || err != nil {
		return "", "", err
	}
	log.Infof("Importing data from seed reconfiguration secret %s", secretRef)

	secretSeedReconfiguration := imagebased.SeedReconfiguration{}
	if err := json.Unmarshal(secretSeedReconfigurationData, &secretSeedReconfiguration); err != nil {
		return "", "", fmt.Errorf("failed to decode secret seed reconfiguration: %w", err)
	}

	return secretSeedReconfiguration.ClusterID, secretSeedReconfiguration.InfraID, nil
}

func (r *Credentials) createOrUpdateClusterCredentialSecret(ctx context.Context, log logrus.FieldLogger, cd *hivev1.ClusterDeployment, name string, data map[string][]byte, secretType string) error {
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
		log.WithError(err).Errorf("error getting GVK for clusterdeployment")
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
	log.Infof("%s secret %s", secretType, op)
	return nil
}

func KubeconfigSecretName(clusterDeploymentName string) string {
	return clusterDeploymentName + "-admin-kubeconfig"
}

func KubeadminPasswordSecretName(clusterDeploymentName string) string {
	return clusterDeploymentName + "-admin-password"
}

func SeedReconfigurationSecretName(clusterDeploymentName string) string {
	return clusterDeploymentName + "-seed-reconfiguration"
}

func (r *Credentials) getSecretContent(ctx context.Context, ref types.NamespacedName, key string) ([]byte, bool, error) {
	secret := &corev1.Secret{}
	err := r.Get(ctx, ref, secret)
	if errors.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("failed to get secret %s: %w", ref, err)
	}
	val, exists := secret.Data[key]
	return val, exists && len(val) != 0, nil
}

// ClusterIdentitySecrets returns the value of the three secrets representing the cluster identity (kubeconfig, kubeadmin-password, and seed-reconfiguration
// `exist` is false if any of the secrets are not present or if the required key in any secret has no content
// `err` is set if an error is encountered retrieving any secret
func (r *Credentials) ClusterIdentitySecrets(ctx context.Context, cd *hivev1.ClusterDeployment) (kubeconfig []byte, kubeadminPassword []byte, seedReconfig []byte, exist bool, err error) {
	var present bool

	ref := types.NamespacedName{Namespace: cd.Namespace, Name: KubeconfigSecretName(cd.Name)}
	kubeconfig, present, err = r.getSecretContent(ctx, ref, "kubeconfig")
	if !present || err != nil {
		return
	}

	ref = types.NamespacedName{Namespace: cd.Namespace, Name: KubeadminPasswordSecretName(cd.Name)}
	kubeadminPassword, present, err = r.getSecretContent(ctx, ref, kubeAdminKey)
	if !present || err != nil {
		return
	}

	ref = types.NamespacedName{Namespace: cd.Namespace, Name: SeedReconfigurationSecretName(cd.Name)}
	seedReconfig, present, err = r.getSecretContent(ctx, ref, SeedReconfigurationFileName)
	if !present || err != nil {
		return
	}

	return kubeconfig, kubeadminPassword, seedReconfig, true, nil
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
