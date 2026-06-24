/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"

	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/sirupsen/logrus"

	"github.com/openshift/image-based-install-operator/api/v1alpha1"
)

func getDataImage(ctx context.Context, c client.Client, namespace, name string) (*bmh_v1alpha1.DataImage, error) {
	dataImage := &bmh_v1alpha1.DataImage{}
	key := types.NamespacedName{Name: name, Namespace: namespace}
	if err := c.Get(ctx, key, dataImage); err != nil {
		return nil, err
	}
	return dataImage, nil
}

func deleteDataImage(ctx context.Context, c client.Client, log logrus.FieldLogger, dataImageRef types.NamespacedName) (*bmh_v1alpha1.DataImage, error) {
	dataImage := &bmh_v1alpha1.DataImage{}
	if err := c.Get(ctx, dataImageRef, dataImage); err != nil {
		if k8sapierrors.IsNotFound(err) {
			log.Infof("Can't find DataImage from BareMetalHost %s/%s, Nothing to remove", dataImageRef.Namespace, dataImageRef.Name)
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get DataImage %s/%s: %w", dataImageRef.Namespace, dataImageRef.Name, err)
	}

	log.Infof("Deleting DataImage %s/%s, deletion may take some time since a BMH restart is required", dataImageRef.Namespace, dataImageRef.Name)
	if err := c.Delete(ctx, dataImage); err != nil {
		return dataImage, err
	}
	return dataImage, nil
}

func removeBMHDataImage(ctx context.Context, c client.Client, log logrus.FieldLogger, bmhRef types.NamespacedName) (*bmh_v1alpha1.DataImage, error) {
	dataImage, err := deleteDataImage(ctx, c, log, bmhRef)
	if err != nil || dataImage == nil {
		return dataImage, err
	}

	bmh := &bmh_v1alpha1.BareMetalHost{}
	if err := c.Get(ctx, bmhRef, bmh); err != nil {
		if k8sapierrors.IsNotFound(err) {
			log.Warnf("Referenced BareMetalHost %s/%s does not exist, not waiting for dataImage deletion", bmhRef.Namespace, bmhRef.Name)
			return nil, nil
		}
		return dataImage, fmt.Errorf("failed to get BareMetalHost %s/%s: %w", bmhRef.Namespace, bmhRef.Name, err)
	}
	return dataImage, attachAndRebootBMH(ctx, c, log, bmh)
}

func attachAndRebootBMH(ctx context.Context, c client.Client, log logrus.FieldLogger, bmh *bmh_v1alpha1.BareMetalHost) error {
	patch := client.MergeFrom(bmh.DeepCopy())
	dirty := false
	if annotationExists(&bmh.ObjectMeta, detachedAnnotation) {
		log.Infof("Removing Detached annotation if exists on BareMetalHost %s/%s", bmh.Namespace, bmh.Name)
		delete(bmh.ObjectMeta.Annotations, detachedAnnotation)
		dirty = true
	}

	if setAnnotationIfNotExists(&bmh.ObjectMeta, rebootAnnotation, rebootAnnotationValue) {
		log.Infof("Adding reboot annotation to BareMetalHost %s/%s", bmh.Namespace, bmh.Name)
		dirty = true
	}
	if dirty {
		return c.Patch(ctx, bmh, patch)
	}
	return nil
}

func getBMH(ctx context.Context, c client.Client, bmhRef *v1alpha1.BareMetalHostReference) (*bmh_v1alpha1.BareMetalHost, error) {
	bmh := &bmh_v1alpha1.BareMetalHost{}
	key := types.NamespacedName{
		Name:      bmhRef.Name,
		Namespace: bmhRef.Namespace,
	}
	if err := c.Get(ctx, key, bmh); err != nil {
		return nil, err
	}

	return bmh, nil
}

func setAnnotationIfNotExists(meta *metav1.ObjectMeta, key string, value string) bool {
	if meta.Annotations == nil {
		meta.Annotations = make(map[string]string)
	}
	if _, ok := meta.Annotations[key]; !ok {
		meta.Annotations[key] = value
		return true
	}
	return false
}

func annotationExists(meta *metav1.ObjectMeta, key string) bool {
	if meta.Annotations == nil {
		return false
	}
	_, ok := meta.Annotations[key]
	return ok
}
