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
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	gtypes "github.com/onsi/gomega/types"
	machineproviderv1 "github.com/openshift/cluster-api-provider-equinix-metal/pkg/apis/equinixmetal/v1beta1"
	machinev1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	"github.com/packethost/packngo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type fakePlanService struct {
	plans []packngo.Plan
}

func (f *fakePlanService) getter() PlanServiceGetter {
	return func(name, apiKey string) packngo.PlanService {
		return f
	}
}

func (f *fakePlanService) List(opts *packngo.ListOptions) ([]packngo.Plan, *packngo.Response, error) {
	return f.plans, nil, nil
}

var _ = Describe("Reconciler", func() {
	var c client.Client
	var stopMgr context.CancelFunc
	var fakeRecorder *record.FakeRecorder
	var namespace *corev1.Namespace

	BeforeEach(func() {
		mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
		Expect(err).ToNot(HaveOccurred())

		fakePlanService := fakePlanService{
			plans: []packngo.Plan{
				{
					ID:   "test",
					Slug: "c3.small.x86",
					Specs: &packngo.Specs{
						Cpus: []*packngo.Cpus{
							{
								Count: 1,
								Type:  "Intel(R) Xeon(R) E-2278G CPU @ 3.40GHz",
							},
						},
						Memory: &packngo.Memory{
							Total: "32GB",
						},
					},
				},
				{
					ID:   "test",
					Slug: "m2.xlarge.x86",
					Specs: &packngo.Specs{
						Cpus: []*packngo.Cpus{
							{
								Count: 2,
								Type:  "Intel Scalable Gold 5120 28-Core Processor @ 2.2GHz",
							},
						},
						Memory: &packngo.Memory{
							Total: "384GB",
						},
					},
				},
			},
		}

		r := Reconciler{
			Client:            mgr.GetClient(),
			Log:               log.Log,
			PlanServiceGetter: fakePlanService.getter(),
		}
		Expect(r.SetupWithManager(mgr, controller.Options{})).To(Succeed())

		fakeRecorder = record.NewFakeRecorder(1)
		r.recorder = fakeRecorder

		c = mgr.GetClient()
		stopMgr = StartTestManager(mgr)

		namespace = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "mhc-test-"}}
		Expect(c.Create(ctx, namespace)).To(Succeed())
	})

	AfterEach(func() {
		Expect(deleteMachineSets(c, namespace.Name)).To(Succeed())
		stopMgr()
	})

	type reconcileTestCase = struct {
		machineType         string
		existingAnnotations map[string]string
		expectedAnnotations map[string]string
		expectedEvents      []string
	}

	DescribeTable("when reconciling MachineSets", func(rtc reconcileTestCase) {
		machineSet, err := newTestMachineSet(namespace.Name, rtc.machineType, rtc.existingAnnotations)
		Expect(err).ToNot(HaveOccurred())

		Expect(c.Create(ctx, machineSet)).To(Succeed())

		Eventually(func() map[string]string {
			m := &machinev1.MachineSet{}
			key := client.ObjectKey{Namespace: machineSet.Namespace, Name: machineSet.Name}
			err := c.Get(ctx, key, m)
			if err != nil {
				return nil
			}
			annotations := m.GetAnnotations()
			if annotations != nil {
				return annotations
			}
			// Return an empty map to distinguish between empty annotations and errors
			return make(map[string]string)
		}, timeout).Should(Equal(rtc.expectedAnnotations))

		// Check which event types were sent
		Eventually(fakeRecorder.Events, timeout).Should(HaveLen(len(rtc.expectedEvents)))
		receivedEvents := []string{}
		eventMatchers := []gtypes.GomegaMatcher{}
		for _, ev := range rtc.expectedEvents {
			receivedEvents = append(receivedEvents, <-fakeRecorder.Events)
			eventMatchers = append(eventMatchers, ContainSubstring(fmt.Sprintf(" %s ", ev)))
		}
		Expect(receivedEvents).To(ConsistOf(eventMatchers))
	},
		Entry("with no vmSize set", reconcileTestCase{
			machineType:         "",
			existingAnnotations: make(map[string]string),
			expectedAnnotations: make(map[string]string),
			expectedEvents:      []string{"ReconcileError"},
		}),
		Entry("with a c3.small", reconcileTestCase{
			machineType:         "c3.small",
			existingAnnotations: make(map[string]string),
			expectedAnnotations: map[string]string{
				cpuKey:    "1",
				memoryKey: "32000",
			},
			expectedEvents: []string{},
		}),
		Entry("with a m2.xlarge", reconcileTestCase{
			machineType:         "m2.xlarge",
			existingAnnotations: make(map[string]string),
			expectedAnnotations: map[string]string{
				cpuKey:    "2",
				memoryKey: "384000",
			},
			expectedEvents: []string{},
		}),
		Entry("with existing annotations", reconcileTestCase{
			machineType: "c3.small",
			existingAnnotations: map[string]string{
				"existing": "annotation",
				"annother": "existingAnnotation",
			},
			expectedAnnotations: map[string]string{
				"existing": "annotation",
				"annother": "existingAnnotation",
				cpuKey:     "1",
				memoryKey:  "32000",
			},
			expectedEvents: []string{},
		}),
		Entry("with an invalid machineType", reconcileTestCase{
			machineType: "r4.notFound",
			existingAnnotations: map[string]string{
				"existing": "annotation",
				"annother": "existingAnnotation",
			},
			expectedAnnotations: map[string]string{
				"existing": "annotation",
				"annother": "existingAnnotation",
			},
			expectedEvents: []string{"ReconcileError"},
		}),
	)
})

func deleteMachineSets(c client.Client, namespaceName string) error {
	machineSets := &machinev1.MachineSetList{}

	err := c.List(ctx, machineSets, client.InNamespace(namespaceName))
	if err != nil {
		return err
	}

	for i := range machineSets.Items {
		if err := c.Delete(ctx, &machineSets.Items[i]); err != nil {
			return err
		}
	}

	Eventually(func() error {
		machineSets := &machinev1.MachineSetList{}

		err := c.List(ctx, machineSets)
		if err != nil {
			return err
		}

		if len(machineSets.Items) > 0 {
			return errors.New("MachineSets not deleted")
		}

		return nil
	}, timeout).Should(Succeed())

	return nil
}

func TestReconcile(t *testing.T) {
	testCases := []struct {
		name                string
		machineType         string
		plans               []packngo.Plan
		existingAnnotations map[string]string
		expectedAnnotations map[string]string
		expectErr           bool
	}{
		{
			name:        "with no machineType set",
			machineType: "",
			plans: []packngo.Plan{
				{
					ID:   "test",
					Slug: "c3.large.x86",
				},
			},
			existingAnnotations: make(map[string]string),
			expectedAnnotations: make(map[string]string),
			expectErr:           true,
		},
		{
			name:        "with a c3.small",
			machineType: "c3.small",
			plans: []packngo.Plan{
				{
					ID:   "test",
					Slug: "c3.small.x86",
					Specs: &packngo.Specs{
						Cpus: []*packngo.Cpus{
							{
								Count: 1,
								Type:  "Intel(R) Xeon(R) E-2278G CPU @ 3.40GHz",
							},
						},
						Memory: &packngo.Memory{
							Total: "32GB",
						},
					},
				},
			},
			existingAnnotations: make(map[string]string),
			expectedAnnotations: map[string]string{
				cpuKey:    "1",
				memoryKey: "32000",
			},
			expectErr: false,
		},
		{
			name:        "with a m2.xlarge",
			machineType: "m2.xlarge",
			plans: []packngo.Plan{
				{
					ID:   "test",
					Slug: "m2.xlarge.x86",
					Specs: &packngo.Specs{
						Cpus: []*packngo.Cpus{
							{
								Count: 2,
								Type:  "Intel Scalable Gold 5120 28-Core Processor @ 2.2GHz",
							},
						},
						Memory: &packngo.Memory{
							Total: "384GB",
						},
					},
				},
			},
			existingAnnotations: make(map[string]string),
			expectedAnnotations: map[string]string{
				cpuKey:    "2",
				memoryKey: "384000",
			},
			expectErr: false,
		},
		{
			name:        "with existing annotations",
			machineType: "c3.small",
			plans: []packngo.Plan{
				{
					ID:   "test",
					Slug: "c3.small.x86",
					Specs: &packngo.Specs{
						Cpus: []*packngo.Cpus{
							{
								Count: 1,
								Type:  "Intel(R) Xeon(R) E-2278G CPU @ 3.40GHz",
							},
						},
						Memory: &packngo.Memory{
							Total: "32GB",
						},
					},
				},
			},
			existingAnnotations: map[string]string{
				"existing": "annotation",
				"annother": "existingAnnotation",
			},
			expectedAnnotations: map[string]string{
				"existing": "annotation",
				"annother": "existingAnnotation",
				cpuKey:     "1",
				memoryKey:  "32000",
			},
			expectErr: false,
		},
		{
			name:        "with an invalid machineType",
			machineType: "r4.notFound",
			plans:       []packngo.Plan{},
			existingAnnotations: map[string]string{
				"existing": "annotation",
				"annother": "existingAnnotation",
			},
			expectedAnnotations: map[string]string{
				"existing": "annotation",
				"annother": "existingAnnotation",
			},
			expectErr: true,
		},
	}

	for i := range testCases {
		tc := testCases[i]
		t.Run(tc.name, func(tt *testing.T) {
			g := NewWithT(tt)

			fakePlanService := fakePlanService{
				plans: tc.plans,
			}

			r := &Reconciler{
				PlanServiceGetter: fakePlanService.getter(),
			}

			machineSet, err := newTestMachineSet("default", tc.machineType, tc.existingAnnotations)
			g.Expect(err).ToNot(HaveOccurred())

			_, err = r.reconcile(machineSet)
			g.Expect(err != nil).To(Equal(tc.expectErr))
			g.Expect(machineSet.Annotations).To(Equal(tc.expectedAnnotations))
		})
	}
}

func newTestMachineSet(namespace string, machineType string, existingAnnotations map[string]string) (*machinev1.MachineSet, error) {
	// Copy anntotations map so we don't modify the input
	annotations := make(map[string]string)
	for k, v := range existingAnnotations {
		annotations[k] = v
	}

	machineProviderSpec := &machineproviderv1.EquinixMetalMachineProviderSpec{
		MachineType: machineType,
	}

	providerSpec, err := providerSpecFromMachine(machineProviderSpec)
	if err != nil {
		return nil, err
	}

	return &machinev1.MachineSet{
		ObjectMeta: metav1.ObjectMeta{
			Annotations:  annotations,
			GenerateName: "test-machineset-",
			Namespace:    namespace,
		},
		Spec: machinev1.MachineSetSpec{
			Template: machinev1.MachineTemplateSpec{
				Spec: machinev1.MachineSpec{
					ProviderSpec: providerSpec,
				},
			},
		},
	}, nil
}

func providerSpecFromMachine(in *machineproviderv1.EquinixMetalMachineProviderSpec) (machinev1.ProviderSpec, error) {
	bytes, err := json.Marshal(in)
	if err != nil {
		return machinev1.ProviderSpec{}, err
	}

	return machinev1.ProviderSpec{
		Value: &runtime.RawExtension{Raw: bytes},
	}, nil
}
