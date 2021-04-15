/*
Copyright The Kubernetes Authors.
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

package util

import (
	"context"
	"fmt"

	"github.com/openshift/cluster-api-provider-equinix-metal/pkg/apis/equinixmetal/v1beta1"
	machineapierros "github.com/openshift/machine-api-operator/pkg/controller/machine"
	apicorev1 "k8s.io/api/core/v1"
	apimachineryerrors "k8s.io/apimachinery/pkg/api/errors"
	controllerclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	credentialsSecretKey = "api_key"
)

// This expects the https://github.com/openshift/cloud-credential-operator to make a secret
// with a api_key Key content available. E.g:
//
//apiVersion: v1
//kind: Secret
//metadata:
//  name: equinixmetal-sa
//  namespace: openshift-machine-api
//type: Opaque
//data:
//  api_key: <Your Equinix Metal API Key>
func GetCredentialsSecret(coreClient controllerclient.Client, namespace string, spec v1beta1.EquinixMetalMachineProviderSpec) (string, error) {
	if spec.CredentialsSecret == nil {
		return "", nil
	}
	var credentialsSecret apicorev1.Secret

	if err := coreClient.Get(context.Background(), controllerclient.ObjectKey{Namespace: namespace, Name: spec.CredentialsSecret.Name}, &credentialsSecret); err != nil {
		if apimachineryerrors.IsNotFound(err) {
			machineapierros.InvalidMachineConfiguration("credentials secret %q in namespace %q not found: %v", spec.CredentialsSecret.Name, namespace, err.Error())
		}
		return "", fmt.Errorf("error getting credentials secret %q in namespace %q: %v", spec.CredentialsSecret.Name, namespace, err)
	}
	data, exists := credentialsSecret.Data[credentialsSecretKey]
	if !exists {
		return "", machineapierros.InvalidMachineConfiguration("secret %v/%v does not have %q field set. Thus, no credentials applied when creating an instance", namespace, spec.CredentialsSecret.Name, credentialsSecretKey)
	}

	return string(data), nil
}
