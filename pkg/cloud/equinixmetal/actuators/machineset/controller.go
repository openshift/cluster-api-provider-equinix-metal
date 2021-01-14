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

package machineset

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	humanize "github.com/dustin/go-humanize"
	"github.com/go-logr/logr"
	providerconfigv1 "github.com/openshift/cluster-api-provider-equinix-metal/pkg/apis/equinixmetal/v1beta1"
	"github.com/openshift/cluster-api-provider-equinix-metal/pkg/cloud/equinixmetal/actuators/util"
	machinev1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	mapierrors "github.com/openshift/machine-api-operator/pkg/controller/machine"
	"github.com/packethost/packngo"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
)

const (
	// This exposes compute information based on the providerSpec input.
	// This is needed by the autoscaler to foresee upcoming capacity when scaling from zero.
	// https://github.com/openshift/enhancements/pull/186
	cpuKey     = "machine.openshift.io/vCPU"
	memoryKey  = "machine.openshift.io/memoryMb"
	clientName = "OpenShift-Provider-v1beta1"
)

type PlanServiceGetter = func(name, apiKey string) packngo.PlanService

func RealPlanClient(name, apiKey string) packngo.PlanService {
	return packngo.NewClientWithAuth(name, apiKey, nil).Plans
}

// Reconciler reconciles machineSets.
type Reconciler struct {
	Client            client.Client
	Log               logr.Logger
	PlanServiceGetter PlanServiceGetter

	recorder record.EventRecorder
	scheme   *runtime.Scheme
}

// SetupWithManager creates a new controller for a manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager, options controller.Options) error {
	_, err := ctrl.NewControllerManagedBy(mgr).
		For(&machinev1.MachineSet{}).
		WithOptions(options).
		Build(r)

	if err != nil {
		return fmt.Errorf("failed setting up with a controller manager: %w", err)
	}

	r.recorder = mgr.GetEventRecorderFor("machineset-controller")
	r.scheme = mgr.GetScheme()

	return nil
}

// Reconcile implements controller runtime Reconciler interface.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Log.WithValues("machineset", req.Name, "namespace", req.Namespace)
	logger.V(3).Info("Reconciling")

	machineSet := &machinev1.MachineSet{}
	if err := r.Client.Get(ctx, req.NamespacedName, machineSet); err != nil {
		if apierrors.IsNotFound(err) {
			// Object not found, return. Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	// Ignore deleted MachineSets, this can happen when foregroundDeletion
	// is enabled
	if !machineSet.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	originalMachineSetToPatch := client.MergeFrom(machineSet.DeepCopy())

	result, err := r.reconcile(machineSet)
	if err != nil {
		logger.Error(err, "Failed to reconcile MachineSet")
		r.recorder.Eventf(machineSet, corev1.EventTypeWarning, "ReconcileError", "%v", err)
		// we don't return here so we want to attempt to patch the machine regardless of an error.
	}

	if err := r.Client.Patch(ctx, machineSet, originalMachineSetToPatch); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to patch machineSet: %v", err)
	}

	if isInvalidConfigurationError(err) {
		// For situations where requeuing won't help we don't return error.
		// https://github.com/kubernetes-sigs/controller-runtime/issues/617
		return result, nil
	}
	return result, err
}

func isInvalidConfigurationError(err error) bool {
	switch t := err.(type) {
	case *mapierrors.MachineError:
		if t.Reason == machinev1.InvalidConfigurationMachineError {
			return true
		}
	}

	return false
}

func (r *Reconciler) reconcile(machineSet *machinev1.MachineSet) (ctrl.Result, error) {
	providerSpec, err := getproviderConfig(machineSet)
	if err != nil {
		return ctrl.Result{}, mapierrors.InvalidMachineConfiguration("failed to get providerConfig: %v", err)
	}

	apiKey, err := util.GetCredentialsSecret(r.Client, machineSet.GetNamespace(), *providerSpec)
	if err != nil {
		return ctrl.Result{}, err
	}

	plans, _, err := r.PlanServiceGetter(clientName, apiKey).List(&packngo.ListOptions{})
	if err != nil {
		return ctrl.Result{}, err
	}

	for _, plan := range plans {
		if strings.TrimSuffix(providerSpec.MachineType, ".x86") == strings.TrimSuffix(plan.Slug, ".x86") {
			// TODO: this currently returns physical CPUs, need to figure out how to map to core count or similar that is more useful
			numCPUs := 0
			for _, cpu := range plan.Specs.Cpus {
				numCPUs += cpu.Count
			}

			n, err := humanize.ParseBytes(plan.Specs.Memory.Total)
			if err != nil {
				return ctrl.Result{}, err
			}

			setAnnotations(machineSet, numCPUs, n/(1000*1000))

			return ctrl.Result{}, nil
		}
	}

	return ctrl.Result{}, fmt.Errorf("plan not found")
}

func setAnnotations(machineSet *machinev1.MachineSet, numCPUs int, memory uint64) {
	if machineSet.Annotations == nil {
		machineSet.Annotations = make(map[string]string)
	}

	machineSet.Annotations[cpuKey] = strconv.Itoa(numCPUs)
	machineSet.Annotations[memoryKey] = strconv.FormatUint(memory, 10)
}

func getproviderConfig(machineSet *machinev1.MachineSet) (*providerconfigv1.EquinixMetalMachineProviderSpec, error) {
	return providerconfigv1.ProviderSpecFromRawExtension(machineSet.Spec.Template.Spec.ProviderSpec.Value)
}
