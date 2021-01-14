package machine

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/cluster-api-provider-equinix-metal/pkg/apis/equinixmetal/v1beta1"
	machinev1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	machinecontroller "github.com/openshift/machine-api-operator/pkg/controller/machine"
	"github.com/openshift/machine-api-operator/pkg/metrics"
	"github.com/packethost/packngo"
	corev1 "k8s.io/api/core/v1"
	apimachineryerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	userDataSecretKey   = "userData"
	requeueAfterSeconds = 20
)

// Reconciler are list of services required by machine actuator, easy to create a fake.
type Reconciler struct {
	*machineScope
}

// NewReconciler populates all the services based on input scope.
func newReconciler(scope *machineScope) *Reconciler {
	return &Reconciler{
		scope,
	}
}

// Create creates machine if and only if machine exists, handled by cluster-api.
func (r *Reconciler) create() error {
	if err := validateMachine(*r.machine, *r.providerSpec); err != nil {
		return machinecontroller.InvalidMachineConfiguration("failed validating machine provider spec: %v", err)
	}

	// userData
	userData, err := r.getCustomUserData()
	if err != nil {
		return fmt.Errorf("error getting custom user data: %v", err)
	}

	klog.Infof("Userdata: %v", userData)

	req := &packngo.DeviceCreateRequest{
		Hostname:      r.machine.Name,
		Facility:      []string{r.providerSpec.Facility},
		ProjectID:     r.providerSpec.ProjectID,
		UserData:      userData,
		IPXEScriptURL: r.providerSpec.IPXEScriptURL,
		CustomData:    r.providerSpec.CustomData,
		Plan:          r.providerSpec.MachineType,
		OS:            r.providerSpec.OS,
		BillingCycle:  r.providerSpec.BillingCycle,
		Tags:          r.providerSpec.Tags,
	}

	deviceService := r.deviceServiceGetter(clientName, r.apiKey)

	if _, _, err := deviceService.Create(req); err != nil {
		if reconcileWithCloudError := r.reconcileMachineWithCloudState(&v1beta1.EquinixMetalMachineProviderCondition{
			Type:    v1beta1.MachineCreated,
			Reason:  machineCreationFailedReason,
			Message: err.Error(),
			Status:  corev1.ConditionFalse,
		}); reconcileWithCloudError != nil {
			klog.Errorf("Failed to reconcile machine with cloud state: %v", reconcileWithCloudError)
		}

		return fmt.Errorf("failed to create instance via device service: %v", err)
	}

	return r.reconcileMachineWithCloudState(nil)
}

func (r *Reconciler) update() error {
	if err := validateMachine(*r.machine, *r.providerSpec); err != nil {
		return machinecontroller.InvalidMachineConfiguration("failed validating machine provider spec: %v", err)
	}

	return r.reconcileMachineWithCloudState(nil)
}

// reconcileMachineWithCloudState reconcile machineSpec and status with the latest cloud state
// if a failedCondition is passed it updates the providerStatus.Conditions and return
// otherwise it fetches the relevant cloud instance and reconcile the rest of the fields.
func (r *Reconciler) reconcileMachineWithCloudState(failedCondition *v1beta1.EquinixMetalMachineProviderCondition) error {
	klog.Infof("%s: Reconciling machine object with cloud state", r.machine.Name)
	if failedCondition != nil {
		r.providerStatus.Conditions = reconcileProviderConditions(r.providerStatus.Conditions, *failedCondition)
		return nil
	} else {
		device, err := r.getDevice()
		if err != nil {
			return fmt.Errorf("failed to get instance via device service: %v", err)
		}

		nodeAddresses := []corev1.NodeAddress{
			{
				Type:    corev1.NodeInternalIP,
				Address: device.GetNetworkInfo().PrivateIPv4,
			},
			{
				Type:    corev1.NodeExternalIP,
				Address: device.GetNetworkInfo().PublicIPv4,
			},
		}

		providerID := fmt.Sprintf("packet://%s", device.ID)
		r.machine.Spec.ProviderID = &providerID
		r.machine.Status.Addresses = nodeAddresses
		r.providerStatus.InstanceState = &device.State
		r.providerStatus.InstanceID = &device.ID
		succeedCondition := v1beta1.EquinixMetalMachineProviderCondition{
			Type:    v1beta1.MachineCreated,
			Reason:  machineCreationSucceedReason,
			Message: machineCreationSucceedMessage,
			Status:  corev1.ConditionTrue,
		}
		r.providerStatus.Conditions = reconcileProviderConditions(r.providerStatus.Conditions, succeedCondition)

		r.setMachineCloudProviderSpecifics(device)

		if device.State != "active" {
			klog.Infof("%s: machine status is %q, requeuing...", r.machine.Name, device.State)
			return &machinecontroller.RequeueAfterError{RequeueAfter: requeueAfterSeconds * time.Second}
		}
	}

	return nil
}

func (r *Reconciler) setMachineCloudProviderSpecifics(instance *packngo.Device) {
	if r.machine.Labels == nil {
		r.machine.Labels = make(map[string]string)
	}

	if r.machine.Annotations == nil {
		r.machine.Annotations = make(map[string]string)
	}

	// r.machine.Annotations[machinecontroller.MachineInstanceStateAnnotationName] = instance.Status
	// TODO(jchaloup): detect all three from instance rather than
	// always assuming it's the same as what is specified in the provider spec
	r.machine.Labels[machinecontroller.MachineInstanceTypeLabelName] = r.providerSpec.MachineType
	r.machine.Labels[machinecontroller.MachineRegionLabelName] = r.providerSpec.Facility
}

func (r *Reconciler) getCustomUserData() (string, error) {
	if r.providerSpec.UserDataSecret == nil {
		return "", nil
	}

	var userDataSecret corev1.Secret

	if err := r.coreClient.Get(context.Background(), client.ObjectKey{Namespace: r.machine.GetNamespace(), Name: r.providerSpec.UserDataSecret.Name}, &userDataSecret); err != nil {
		if apimachineryerrors.IsNotFound(err) {
			return "", machinecontroller.InvalidMachineConfiguration("user data secret %q in namespace %q not found: %v", r.providerSpec.UserDataSecret.Name, r.machine.GetNamespace(), err)
		}

		return "", fmt.Errorf("error getting user data secret %q in namespace %q: %v", r.providerSpec.UserDataSecret.Name, r.machine.GetNamespace(), err)
	}

	data, exists := userDataSecret.Data[userDataSecretKey]
	if !exists {
		return "", machinecontroller.InvalidMachineConfiguration("secret %v/%v does not have %q field set. Thus, no user data applied when creating an instance", r.machine.GetNamespace(), r.providerSpec.UserDataSecret.Name, userDataSecretKey)
	}

	return string(data), nil
}

func validateMachine(machine machinev1.Machine, providerSpec v1beta1.EquinixMetalMachineProviderSpec) error {
	// TODO (alberto): First validation should happen via webhook before the object is persisted.
	// This is a complementary validation to fail early in case of lacking proper webhook validation.
	// Default values can also be set here
	if machine.Labels[machinev1.MachineClusterIDLabel] == "" {
		return machinecontroller.InvalidMachineConfiguration("machine is missing %q label", machinev1.MachineClusterIDLabel)
	}

	return nil
}

// Returns true if machine exists.
func (r *Reconciler) exists() (bool, error) {
	device, err := r.getDevice()
	if err != nil {
		return false, err
	}

	if device == nil {
		klog.Infof("%s: Machine does not exist", r.machine.Name)

		return false, nil
	}

	return true, nil
}

func (r *Reconciler) getDevice() (*packngo.Device, error) {
	if err := validateMachine(*r.machine, *r.providerSpec); err != nil {
		return nil, fmt.Errorf("failed validating machine provider spec: %v", err)
	}

	var device *packngo.Device

	deviceService := r.deviceServiceGetter(clientName, r.apiKey)

	devices, _, err := deviceService.List(r.projectID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list devices via device service: %v", err)
	}

	for i, d := range devices {
		// TODO: also test tags
		if d.Hostname == r.machine.Name && d.Facility.Code == r.providerSpec.Facility {
			device = &devices[i]

			break
		}
	}

	return device, nil
}

func (r *Reconciler) delete() error {
	device, err := r.getDevice()
	if err != nil {
		return err
	}

	if device == nil {
		klog.Infof("%s: Machine not found during delete, skipping", r.machine.Name)

		return nil
	}

	deviceService := r.deviceServiceGetter(clientName, r.apiKey)

	if _, err := deviceService.Delete(device.ID, false); err != nil {
		metrics.RegisterFailedInstanceDelete(&metrics.MachineLabels{
			Name:      r.machine.Name,
			Namespace: r.machine.Namespace,
			Reason:    err.Error(),
		})

		return fmt.Errorf("failed to delete instance via device service: %v", err)
	}

	klog.Infof("%s: machine status is exists, requeuing...", r.machine.Name)

	return &machinecontroller.RequeueAfterError{RequeueAfter: requeueAfterSeconds * time.Second}
}
