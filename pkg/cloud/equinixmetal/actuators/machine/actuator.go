package machine

// This is a thin layer to implement the machine actuator interface with cloud provider details.
// The lifetime of scope and reconciler is a machine actuator operation.
// when scope is closed, it will persist to etcd the given machine spec and machine status (if modified).
import (
	"context"
	"fmt"

	machinev1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	controllerclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Actuator is responsible for performing machine reconciliation.
type Actuator struct {
	coreClient    controllerclient.Client
	eventRecorder record.EventRecorder
}

// ActuatorParams holds parameter information for Actuator.
type ActuatorParams struct {
	CoreClient    controllerclient.Client
	EventRecorder record.EventRecorder
}

// NewActuator returns an actuator.
func NewActuator(params ActuatorParams) *Actuator {
	return &Actuator{
		coreClient:    params.CoreClient,
		eventRecorder: params.EventRecorder,
	}
}

// Create creates a machine and is invoked by the machine controller.
func (a *Actuator) Create(ctx context.Context, machine *machinev1.Machine) error {
	klog.Infof("%s: Creating machine", machine.Name)

	return fmt.Errorf("Not yet implemented")
}

func (a *Actuator) Exists(ctx context.Context, machine *machinev1.Machine) (bool, error) {
	klog.Infof("%s: Checking if machine exists", machine.Name)

	return false, fmt.Errorf("Not yet implemented")
}

func (a *Actuator) Update(ctx context.Context, machine *machinev1.Machine) error {
	klog.Infof("%s: Updating machine", machine.Name)

	return fmt.Errorf("Not yet implemented")
}

func (a *Actuator) Delete(ctx context.Context, machine *machinev1.Machine) error {
	klog.Infof("%s: Deleting machine", machine.Name)

	return fmt.Errorf("Not yet implemented")
}
