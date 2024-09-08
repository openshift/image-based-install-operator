package monitor

import (
	"context"
	"testing"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GetClusterInstallStatus", func() {
	var (
		ctx = context.Background()
		log = logrus.New()
		c   client.Client
	)

	BeforeEach(func() {
		scheme := runtime.NewScheme()
		utilruntime.Must(corev1.AddToScheme(scheme))
		utilruntime.Must(configv1.AddToScheme(scheme))
		c = fakeclient.NewClientBuilder().WithScheme(scheme).Build()
	})

	createClusterVersion := func(availableStatus configv1.ConditionStatus) {
		cv := configv1.ClusterVersion{
			ObjectMeta: metav1.ObjectMeta{
				Name: "version",
			},
			Spec: configv1.ClusterVersionSpec{
				ClusterID: "2df3ed12-a142-437d-a398-c551dfd8e9ba",
			},
			Status: configv1.ClusterVersionStatus{
				Conditions: []configv1.ClusterOperatorStatusCondition{{
					Type:               configv1.OperatorAvailable,
					Status:             availableStatus,
					Message:            "message",
					Reason:             "reason",
					LastTransitionTime: metav1.Time{Time: time.Now()},
				}},
			},
		}
		Expect(c.Create(ctx, &cv)).To(Succeed())
	}

	createNode := func(name string, readyStatus corev1.ConditionStatus) {
		node := corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{
					Type:   corev1.NodeReady,
					Status: readyStatus,
				}},
			},
		}
		Expect(c.Create(ctx, &node)).To(Succeed())
	}

	createIBIOStartTimeCM := func(createdAt time.Time) {
		cm := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: corev1.SchemeGroupVersion.String(),
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace:         OcpConfigNamespace,
				Name:              IBIOStartTimeCM,
				CreationTimestamp: metav1.Time{Time: createdAt},
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())
	}

	It("returns true when the cluster version is available and nodes are ready", func() {
		createNode("node1", corev1.ConditionTrue)
		createNode("node2", corev1.ConditionTrue)
		createNode("node3", corev1.ConditionTrue)
		createIBIOStartTimeCM(time.Now())

		createClusterVersion(configv1.ConditionTrue)

		status := GetClusterInstallStatus(ctx, log, c)
		Expect(status.Installed).To(BeTrue())
		Expect(status.ClusterVersionStatus).To(Equal(clusterVersionAvailableMessage))
		Expect(status.NodesStatus).To(Equal(nodesReadyMessage))
	})

	It("returns false when the cluster version is available and a node is not ready", func() {
		createNode("node1", corev1.ConditionFalse)
		createNode("node2", corev1.ConditionTrue)
		createNode("node3", corev1.ConditionTrue)
		createIBIOStartTimeCM(time.Now())
		createClusterVersion(configv1.ConditionTrue)

		status := GetClusterInstallStatus(ctx, log, c)
		Expect(status.Installed).To(BeFalse())
		Expect(status.ClusterVersionStatus).To(Equal(clusterVersionAvailableMessage))
		Expect(status.NodesStatus).ToNot(Equal(nodesReadyMessage))
	})

	It("returns false when the cluster version is not available", func() {
		createNode("node1", corev1.ConditionTrue)
		createIBIOStartTimeCM(time.Now())
		createClusterVersion(configv1.ConditionFalse)

		status := GetClusterInstallStatus(ctx, log, c)
		Expect(status.Installed).To(BeFalse())
		Expect(status.ClusterVersionStatus).ToNot(Equal(clusterVersionAvailableMessage))
		Expect(status.NodesStatus).To(Equal(nodesReadyMessage))
	})

	It("returns false when no nodes exist", func() {
		createIBIOStartTimeCM(time.Now())
		createClusterVersion(configv1.ConditionTrue)

		status := GetClusterInstallStatus(ctx, log, c)
		Expect(status.Installed).To(BeFalse())
		Expect(status.ClusterVersionStatus).To(Equal(clusterVersionAvailableMessage))
		Expect(status.NodesStatus).ToNot(Equal(nodesReadyMessage))
	})

	It("returns false when cm does not exists", func() {
		createNode("node1", corev1.ConditionTrue)
		createClusterVersion(configv1.ConditionTrue)

		status := GetClusterInstallStatus(ctx, log, c)
		Expect(status.Installed).To(BeFalse())
		Expect(status.ClusterVersionStatus).To(ContainSubstring("Failed to get"))
	})

	It("returns false when cm is more than an hour ahead of cvo last transition", func() {
		createNode("node1", corev1.ConditionTrue)
		createIBIOStartTimeCM(time.Now().Add(61 * time.Minute))
		createClusterVersion(configv1.ConditionTrue)

		status := GetClusterInstallStatus(ctx, log, c)
		Expect(status.Installed).To(BeFalse())
		Expect(status.ClusterVersionStatus).To(Equal(clusterVersionNotAvailableMessage))
		Expect(status.NodesStatus).To(Equal(nodesReadyMessage))
	})

	It("returns true when cm creation data is 2 hours before  cvo last transition", func() {
		createNode("node1", corev1.ConditionTrue)
		createIBIOStartTimeCM(time.Now().Add(-120 * time.Minute))
		createClusterVersion(configv1.ConditionTrue)

		status := GetClusterInstallStatus(ctx, log, c)
		Expect(status.Installed).To(BeTrue())
		Expect(status.ClusterVersionStatus).To(Equal(clusterVersionAvailableMessage))
		Expect(status.NodesStatus).To(Equal(nodesReadyMessage))
	})

	It("returns true in case available condition was not updated but progressing was ", func() {
		createNode("node1", corev1.ConditionTrue)
		createIBIOStartTimeCM(time.Now())
		cv := configv1.ClusterVersion{
			ObjectMeta: metav1.ObjectMeta{
				Name: "version",
			},
			Spec: configv1.ClusterVersionSpec{
				ClusterID: "2df3ed12-a142-437d-a398-c551dfd8e9ba",
			},
			Status: configv1.ClusterVersionStatus{
				Conditions: []configv1.ClusterOperatorStatusCondition{{
					Type:               configv1.OperatorAvailable,
					Status:             configv1.ConditionTrue,
					Message:            "message",
					Reason:             "reason",
					LastTransitionTime: metav1.Time{Time: time.Now().Add(-120 * time.Minute)},
				}, {
					Type:               configv1.OperatorProgressing,
					Status:             configv1.ConditionFalse,
					Message:            "message",
					Reason:             "reason",
					LastTransitionTime: metav1.Time{Time: time.Now()},
				},
				},
			},
		}
		Expect(c.Create(ctx, &cv)).To(Succeed())
		status := GetClusterInstallStatus(ctx, log, c)
		Expect(status.Installed).To(BeTrue())
		Expect(status.ClusterVersionStatus).To(Equal(clusterVersionAvailableMessage))
		Expect(status.NodesStatus).To(Equal(nodesReadyMessage))
	})
})

func TestMonitor(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Monitor Suite")
}
