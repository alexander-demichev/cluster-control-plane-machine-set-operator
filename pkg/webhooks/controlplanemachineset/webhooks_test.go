/*
Copyright 2022 Red Hat, Inc.

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

package controlplanemachineset

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	machinev1 "github.com/openshift/api/machine/v1"
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	"github.com/openshift/cluster-control-plane-machine-set-operator/pkg/test"
	"github.com/openshift/cluster-control-plane-machine-set-operator/pkg/test/resourcebuilder"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/envtest/komega"
)

// stringPtr returns a pointer to the string value.
func stringPtr(s string) *string {
	return &s
}

var _ = Describe("Webhooks", func() {
	var mgrCancel context.CancelFunc
	var mgrDone chan struct{}

	var namespaceName string

	BeforeEach(func() {
		By("Setting up a namespace for the test")
		ns := resourcebuilder.Namespace().WithGenerateName("control-plane-machine-set-webhook-").Build()
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		namespaceName = ns.GetName()

		By("Setting up a manager and webhook")
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:             testScheme,
			MetricsBindAddress: "0",
			Port:               testEnv.WebhookInstallOptions.LocalServingPort,
			Host:               testEnv.WebhookInstallOptions.LocalServingHost,
			CertDir:            testEnv.WebhookInstallOptions.LocalServingCertDir,
		})
		Expect(err).ToNot(HaveOccurred(), "Manager should be able to be created")

		wh := &ControlPlaneMachineSetWebhook{}
		Expect(wh.SetupWebhookWithManager(mgr)).To(Succeed(), "Webhook should be able to register with manager")

		By("Starting the manager")
		var mgrCtx context.Context
		mgrCtx, mgrCancel = context.WithCancel(context.Background())
		mgrDone = make(chan struct{})

		go func() {
			defer GinkgoRecover()
			defer close(mgrDone)

			Expect(mgr.Start(mgrCtx)).To(Succeed())
		}()
	})

	AfterEach(func() {
		By("Stopping the manager")
		mgrCancel()
		// Wait for the mgrDone to be closed, which will happen once the mgr has stopped
		<-mgrDone

		test.CleanupResources(Default, ctx, cfg, k8sClient, namespaceName,
			&machinev1beta1.Machine{},
			&machinev1.ControlPlaneMachineSet{},
		)
	})

	Context("on create", func() {
		var builder resourcebuilder.ControlPlaneMachineSetBuilder
		var machineTemplate resourcebuilder.OpenShiftMachineV1Beta1TemplateBuilder

		Context("when validating without failure domains", func() {
			BeforeEach(func() {
				providerSpec := resourcebuilder.AWSProviderSpec().WithAvailabilityZone("us-east-1")
				machineTemplate = resourcebuilder.OpenShiftMachineV1Beta1Template().WithProviderSpecBuilder(providerSpec)
				// Default CPMS builder should be valid, individual tests will override to make it invalid
				builder = resourcebuilder.ControlPlaneMachineSet().WithNamespace(namespaceName).WithMachineTemplateBuilder(machineTemplate)

				machineBuilder := resourcebuilder.Machine().WithNamespace(namespaceName)
				controlPlaneMachineBuilder := machineBuilder.WithGenerateName("control-plane-machine-").AsMaster().WithProviderSpecBuilder(providerSpec)
				By("Creating a selection of Machines")
				for i := 0; i < 3; i++ {
					controlPlaneMachine := controlPlaneMachineBuilder.Build()
					Expect(k8sClient.Create(ctx, controlPlaneMachine)).To(Succeed())
				}
			})

			It("with a valid spec", func() {
				cpms := builder.Build()
				Expect(k8sClient.Create(ctx, cpms)).To(Succeed())
			})

			It("with a disallowed name", func() {
				cpms := builder.WithName("disallowed").Build()
				Expect(apierrors.ReasonForError(k8sClient.Create(ctx, cpms))).To(BeEquivalentTo("name: Invalid value: \"disallowed\": control plane machine set name must be cluster"))
			})

			It("with 4 replicas", func() {
				// This is an openapi validation but it makes sense to include it here as well
				cpms := builder.WithReplicas(4).Build()
				Expect(k8sClient.Create(ctx, cpms)).To(MatchError(ContainSubstring("Unsupported value: 4: supported values: \"3\", \"5\"")))
			})

			It("with mismatched selector and machine labels", func() {
				cpms := builder.WithSelector(metav1.LabelSelector{
					MatchLabels: map[string]string{
						openshiftMachineRoleLabel:            masterMachineRole,
						openshiftMachineTypeLabel:            masterMachineRole,
						machinev1beta1.MachineClusterIDLabel: "cpms-cluster-test-id",
					},
				}).WithMachineTemplateBuilder(
					machineTemplate.WithLabels(map[string]string{
						openshiftMachineRoleLabel:            masterMachineRole,
						openshiftMachineTypeLabel:            masterMachineRole,
						machinev1beta1.MachineClusterIDLabel: "different-id",
					}),
				).Build()

				Expect(apierrors.ReasonForError(k8sClient.Create(ctx, cpms))).To(BeEquivalentTo("spec.template.machines_v1beta1_machine_openshift_io.metadata.labels: Invalid value: map[string]string{\"machine.openshift.io/cluster-api-cluster\":\"different-id\", \"machine.openshift.io/cluster-api-machine-role\":\"master\", \"machine.openshift.io/cluster-api-machine-type\":\"master\"}: selector does not match template labels"))
			})

			It("with no cluster ID label is set", func() {
				cpms := builder.WithSelector(metav1.LabelSelector{
					MatchLabels: map[string]string{
						openshiftMachineRoleLabel: masterMachineRole,
						openshiftMachineTypeLabel: masterMachineRole,
					},
				}).WithMachineTemplateBuilder(
					machineTemplate.WithLabels(map[string]string{
						openshiftMachineRoleLabel: masterMachineRole,
						openshiftMachineTypeLabel: masterMachineRole,
					}),
				).Build()

				Expect(apierrors.ReasonForError(k8sClient.Create(ctx, cpms))).To(BeEquivalentTo("spec.template.machines_v1beta1_machine_openshift_io.metadata.labels: Required value: machine.openshift.io/cluster-api-cluster label is required"))
			})

			It("with no master role label on the template", func() {
				cpms := builder.WithSelector(metav1.LabelSelector{
					MatchLabels: map[string]string{
						openshiftMachineTypeLabel:            masterMachineRole,
						machinev1beta1.MachineClusterIDLabel: "cpms-cluster-test-id",
					},
				}).WithMachineTemplateBuilder(
					machineTemplate.WithLabels(map[string]string{
						openshiftMachineTypeLabel:            masterMachineRole,
						machinev1beta1.MachineClusterIDLabel: "cpms-cluster-test-id",
					}),
				).Build()

				Expect(apierrors.ReasonForError(k8sClient.Create(ctx, cpms))).To(BeEquivalentTo("spec.template.machines_v1beta1_machine_openshift_io.metadata.labels: Required value: machine.openshift.io/cluster-api-machine-role label is required"))
			})

			It("with no master type label on the template", func() {
				cpms := builder.WithSelector(metav1.LabelSelector{
					MatchLabels: map[string]string{
						openshiftMachineRoleLabel:            masterMachineRole,
						machinev1beta1.MachineClusterIDLabel: "cpms-cluster-test-id",
					},
				}).WithMachineTemplateBuilder(
					machineTemplate.WithLabels(map[string]string{
						openshiftMachineRoleLabel:            masterMachineRole,
						machinev1beta1.MachineClusterIDLabel: "cpms-cluster-test-id",
					}),
				).Build()

				Expect(apierrors.ReasonForError(k8sClient.Create(ctx, cpms))).To(BeEquivalentTo("spec.template.machines_v1beta1_machine_openshift_io.metadata.labels: Required value: machine.openshift.io/cluster-api-machine-type label is required"))
			})

			It("with no machine template", func() {
				cpms := builder.WithMachineTemplateBuilder(nil).Build()

				Expect(k8sClient.Create(ctx, cpms)).To(MatchError(ContainSubstring("spec.template.machines_v1beta1_machine_openshift_io: Required value")))
			})

			It("with no machine template value", func() {
				cpms := builder.Build()
				// Leave the union discriminator but set no values.
				cpms.Spec.Template.OpenShiftMachineV1Beta1Machine = nil

				Expect(k8sClient.Create(ctx, cpms)).To(MatchError(ContainSubstring("spec.template.machines_v1beta1_machine_openshift_io: Required value")))
			})
		})

		Context("when validating failure domains on AWS", func() {
			var builder resourcebuilder.ControlPlaneMachineSetBuilder
			var filterSubnet = machinev1.AWSResourceReference{
				Type: machinev1.AWSFiltersReferenceType,
				Filters: &[]machinev1.AWSResourceFilter{{
					Name:   "tag:Name",
					Values: []string{"aws-subnet-12345678"},
				}},
			}

			var filterSubnetDifferent = machinev1.AWSResourceReference{
				Type: machinev1.AWSFiltersReferenceType,
				Filters: &[]machinev1.AWSResourceFilter{{
					Name:   "tag:Name",
					Values: []string{"aws-subnet-different"},
				}},
			}

			var idSubnet = machinev1.AWSResourceReference{
				Type: machinev1.AWSIDReferenceType,
				ID:   stringPtr("subnet-us-east-1c"),
			}

			var usEast1aBuilder = resourcebuilder.AWSFailureDomain().WithAvailabilityZone("us-east-1a").WithSubnet(filterSubnet)
			var usEast1bBuilder = resourcebuilder.AWSFailureDomain().WithAvailabilityZone("us-east-1b").WithSubnet(filterSubnet)
			var usEast1cBuilder = resourcebuilder.AWSFailureDomain().WithAvailabilityZone("us-east-1c").WithSubnet(filterSubnet)
			var usEast1cBuilderWithSubnet = resourcebuilder.AWSFailureDomain().WithAvailabilityZone("us-east-1c").WithSubnet(filterSubnetDifferent)
			var usEast1cBuilderWithIDSubnet = resourcebuilder.AWSFailureDomain().WithAvailabilityZone("us-east-1c").WithSubnet(idSubnet)
			var usEast1dBuilder = resourcebuilder.AWSFailureDomain().WithAvailabilityZone("us-east-1d").WithSubnet(filterSubnet)
			var usEast1eBuilder = resourcebuilder.AWSFailureDomain().WithAvailabilityZone("us-east-1e").WithSubnet(filterSubnet)
			var usEast1fBuilder = resourcebuilder.AWSFailureDomain().WithAvailabilityZone("us-east-1f").WithSubnet(filterSubnet)

			BeforeEach(func() {
				By("Setting up a namespace for the test")
				ns := resourcebuilder.Namespace().WithGenerateName("control-plane-machine-set-webhook-").Build()
				Expect(k8sClient.Create(ctx, ns)).To(Succeed())
				namespaceName = ns.GetName()

				providerSpec := resourcebuilder.AWSProviderSpec()
				machineTemplate = resourcebuilder.OpenShiftMachineV1Beta1Template().WithProviderSpecBuilder(providerSpec)
				machineBuilder := resourcebuilder.Machine().WithNamespace(namespaceName)
				controlPlaneMachineBuilder := machineBuilder.WithGenerateName("control-plane-machine-").AsMaster()
				workerMachineBuilder := machineBuilder.WithGenerateName("worker-machine-").AsWorker()
				machineTemplate := resourcebuilder.OpenShiftMachineV1Beta1Template().WithProviderSpecBuilder(providerSpec)

				builder = resourcebuilder.ControlPlaneMachineSet().WithNamespace(namespaceName).WithMachineTemplateBuilder(machineTemplate)

				var filterSubnet = machinev1beta1.AWSResourceReference{
					Filters: []machinev1beta1.Filter{{
						Name:   "tag:Name",
						Values: []string{"aws-subnet-12345678"},
					}},
				}

				By("Creating a selection of Machines")
				for _, az := range []string{"us-east-1a", "us-east-1b", "us-east-1c"} {
					ps := providerSpec.WithAvailabilityZone(az).WithSubnet(filterSubnet)
					worker := workerMachineBuilder.WithProviderSpecBuilder(ps).Build()
					controlPlane := controlPlaneMachineBuilder.WithProviderSpecBuilder(ps).Build()

					Expect(k8sClient.Create(ctx, worker)).To(Succeed())
					Expect(k8sClient.Create(ctx, controlPlane)).To(Succeed())
				}
				for _, az := range []string{"us-east-1d", "us-east-1e", "us-east-1f"} {
					ps := providerSpec.WithAvailabilityZone(az)
					worker := workerMachineBuilder.WithProviderSpecBuilder(ps).Build()

					Expect(k8sClient.Create(ctx, worker)).To(Succeed())
				}
			})

			It("with a valid failure domains spec", func() {
				cpms := builder.WithMachineTemplateBuilder(machineTemplate.WithFailureDomainsBuilder(
					resourcebuilder.AWSFailureDomains().WithFailureDomainBuilders(
						usEast1aBuilder,
						usEast1bBuilder,
						usEast1cBuilder,
					),
				)).Build()

				Expect(k8sClient.Create(ctx, cpms)).To(Succeed())
			})

			It("with a invalid subnet filter - different value", func() {
				cpms := builder.WithMachineTemplateBuilder(machineTemplate.WithFailureDomainsBuilder(
					resourcebuilder.AWSFailureDomains().WithFailureDomainBuilders(
						usEast1aBuilder,
						usEast1bBuilder,
						usEast1cBuilderWithSubnet,
					),
				)).Build()

				err := k8sClient.Create(ctx, cpms)
				Expect(err).To(MatchError(ContainSubstring("spec.template.machines_v1beta1_machine_openshift_io.failureDomains: Forbidden: control plane machines are using unspecified failure domain(s) [AWSFailureDomain{AvailabilityZone:us-east-1c, Subnet:{Type:filters, Value:&[{Name:tag:Name Values:[aws-subnet-12345678]}]}}")))
				Expect(err).To(MatchError(ContainSubstring("spec.template.machines_v1beta1_machine_openshift_io.failureDomains: Forbidden: no control plane machine is using specified failure domain(s) [AWSFailureDomain{AvailabilityZone:us-east-1c, Subnet:{Type:filters, Value:&[{Name:tag:Name Values:[aws-subnet-different]}]}}")))
			})

			It("with a invalid subnet type - different type", func() {
				cpms := builder.WithMachineTemplateBuilder(machineTemplate.WithFailureDomainsBuilder(
					resourcebuilder.AWSFailureDomains().WithFailureDomainBuilders(
						usEast1aBuilder,
						usEast1bBuilder,
						usEast1cBuilderWithIDSubnet,
					),
				)).Build()

				err := k8sClient.Create(ctx, cpms)
				Expect(err).To(MatchError(ContainSubstring("spec.template.machines_v1beta1_machine_openshift_io.failureDomains: Forbidden: control plane machines are using unspecified failure domain(s) [AWSFailureDomain{AvailabilityZone:us-east-1c, Subnet:{Type:filters, Value:&[{Name:tag:Name Values:[aws-subnet-12345678]}]}}]")))
				Expect(err).To(MatchError(ContainSubstring("spec.template.machines_v1beta1_machine_openshift_io.failureDomains: Forbidden: no control plane machine is using specified failure domain(s) [AWSFailureDomain{AvailabilityZone:us-east-1c, Subnet:{Type:id, Value:subnet-us-east-1c}}]")))
			})

			It("when reducing the availability", func() {
				cpms := builder.WithMachineTemplateBuilder(machineTemplate.WithFailureDomainsBuilder(
					resourcebuilder.AWSFailureDomains().WithFailureDomainBuilders(
						usEast1aBuilder,
					),
				)).Build()

				err := k8sClient.Create(ctx, cpms) // Multiple expects because failure domain ordering is random
				Expect(err).To(MatchError(ContainSubstring("spec.template.machines_v1beta1_machine_openshift_io.failureDomains: Forbidden: control plane machines are using unspecified failure domain(s)")))
				Expect(err).To(MatchError(ContainSubstring("AWSFailureDomain{AvailabilityZone:us-east-1b, Subnet:{Type:filters, Value:&[{Name:tag:Name Values:[aws-subnet-12345678]}]}}")))
				Expect(err).To(MatchError(ContainSubstring("AWSFailureDomain{AvailabilityZone:us-east-1c, Subnet:{Type:filters, Value:&[{Name:tag:Name Values:[aws-subnet-12345678]}]}}")))
			})

			It("when increasing the availability", func() {
				cpms := builder.WithMachineTemplateBuilder(machineTemplate.WithFailureDomainsBuilder(
					resourcebuilder.AWSFailureDomains().WithFailureDomainBuilders(
						usEast1aBuilder,
						usEast1bBuilder,
						usEast1cBuilder,
						usEast1dBuilder,
					),
				)).Build()

				Expect(apierrors.ReasonForError(k8sClient.Create(ctx, cpms))).To(BeEquivalentTo("spec.template.machines_v1beta1_machine_openshift_io.failureDomains: Forbidden: no control plane machine is using specified failure domain(s) [AWSFailureDomain{AvailabilityZone:us-east-1d, Subnet:{Type:filters, Value:&[{Name:tag:Name Values:[aws-subnet-12345678]}]}}]"))
			})

			It("when the availability zones don't match", func() {
				cpms := builder.WithMachineTemplateBuilder(machineTemplate.WithFailureDomainsBuilder(
					resourcebuilder.AWSFailureDomains().WithFailureDomainBuilders(
						usEast1dBuilder,
						usEast1eBuilder,
						usEast1fBuilder,
					),
				)).Build()

				err := k8sClient.Create(ctx, cpms) // Multiple expects because failure domain ordering is random
				Expect(err).To(MatchError(ContainSubstring("spec.template.machines_v1beta1_machine_openshift_io.failureDomains: Forbidden: control plane machines are using unspecified failure domain(s)")))
				Expect(err).To(MatchError(ContainSubstring("AWSFailureDomain{AvailabilityZone:us-east-1a, Subnet:{Type:filters, Value:&[{Name:tag:Name Values:[aws-subnet-12345678]}]}}")))
				Expect(err).To(MatchError(ContainSubstring("AWSFailureDomain{AvailabilityZone:us-east-1b, Subnet:{Type:filters, Value:&[{Name:tag:Name Values:[aws-subnet-12345678]}]}}")))
				Expect(err).To(MatchError(ContainSubstring("AWSFailureDomain{AvailabilityZone:us-east-1c, Subnet:{Type:filters, Value:&[{Name:tag:Name Values:[aws-subnet-12345678]}]}}")))
				Expect(err).To(MatchError(ContainSubstring("spec.template.machines_v1beta1_machine_openshift_io.failureDomains: Forbidden: no control plane machine is using specified failure domain(s)")))
				Expect(err).To(MatchError(ContainSubstring("AWSFailureDomain{AvailabilityZone:us-east-1d, Subnet:{Type:filters, Value:&[{Name:tag:Name Values:[aws-subnet-12345678]}]}}")))
				Expect(err).To(MatchError(ContainSubstring("AWSFailureDomain{AvailabilityZone:us-east-1e, Subnet:{Type:filters, Value:&[{Name:tag:Name Values:[aws-subnet-12345678]}]}}")))
				Expect(err).To(MatchError(ContainSubstring("AWSFailureDomain{AvailabilityZone:us-east-1f, Subnet:{Type:filters, Value:&[{Name:tag:Name Values:[aws-subnet-12345678]}]}}")))
			})
		})
	})

	Context("on update", func() {
		var cpms *machinev1.ControlPlaneMachineSet

		BeforeEach(func() {
			providerSpec := resourcebuilder.AWSProviderSpec().WithAvailabilityZone("us-east-1")
			machineTemplate := resourcebuilder.OpenShiftMachineV1Beta1Template().WithProviderSpecBuilder(providerSpec)
			// Default CPMS builder should be valid
			cpms = resourcebuilder.ControlPlaneMachineSet().WithNamespace(namespaceName).WithMachineTemplateBuilder(machineTemplate).Build()

			machineBuilder := resourcebuilder.Machine().WithNamespace(namespaceName)
			controlPlaneMachineBuilder := machineBuilder.WithGenerateName("control-plane-machine-").AsMaster().WithProviderSpecBuilder(providerSpec)
			By("Creating a selection of Machines")
			for i := 0; i < 3; i++ {
				controlPlaneMachine := controlPlaneMachineBuilder.Build()
				Expect(k8sClient.Create(ctx, controlPlaneMachine)).To(Succeed())
			}

			By("Creating a valid ControlPlaneMachineSet")
			Expect(k8sClient.Create(ctx, cpms)).To(Succeed())
		})

		It("with an update to the providerSpec", func() {
			// Change the providerSpec, expect the update to be successful
			rawProviderSpec := resourcebuilder.AWSProviderSpec().WithAvailabilityZone("us-east-2").BuildRawExtension()

			Eventually(komega.Update(cpms, func() {
				cpms.Spec.Template.OpenShiftMachineV1Beta1Machine.Spec.ProviderSpec.Value = rawProviderSpec
			})).Should(Succeed())
		})

		It("with 4 replicas", func() {
			// This is an openapi validation but it makes sense to include it here as well
			Eventually(komega.Update(cpms, func() {
				four := int32(4)
				cpms.Spec.Replicas = &four
			})).Should(MatchError(ContainSubstring("Unsupported value: 4: supported values: \"3\", \"5\"")))
		})

		It("with 5 replicas", func() {
			// Five replicas is a valid value but the existing CPMS has three replicas
			Eventually(komega.Update(cpms, func() {
				five := int32(5)
				cpms.Spec.Replicas = &five
			})).Should(MatchError(ContainSubstring(`spec.replicas: Forbidden: control plane machine set replicas cannot be changed`)), "Replicas should be immutable")
		})

		It("when modifying the machine labels and the selector still matches", func() {
			Eventually(komega.Update(cpms, func() {
				cpms.Spec.Template.OpenShiftMachineV1Beta1Machine.ObjectMeta.Labels["new"] = "value"
			})).Should(Succeed(), "Machine label updates are allowed provided the selector still matches")
		})

		It("when modifying the machine labels so that the selector no longer matches", func() {
			Eventually(komega.Update(cpms, func() {
				cpms.Spec.Template.OpenShiftMachineV1Beta1Machine.ObjectMeta.Labels = map[string]string{
					"different":                          "labels",
					machinev1beta1.MachineClusterIDLabel: "cpms-cluster-test-id",
					openshiftMachineRoleLabel:            "not-matching-label",
					openshiftMachineTypeLabel:            masterMachineRole,
				}
			})).Should(MatchError(ContainSubstring("selector does not match template labels")), "The selector must always match the machine labels")
		})

		It("when modifying the machine labels to remove the cluster ID label", func() {
			Eventually(komega.Update(cpms, func() {
				delete(cpms.Spec.Template.OpenShiftMachineV1Beta1Machine.ObjectMeta.Labels, machinev1beta1.MachineClusterIDLabel)
			})).Should(MatchError(ContainSubstring("Required value: machine.openshift.io/cluster-api-cluster label is required")), "The labels must always contain a cluster ID label")
		})

		It("when mutating the selector", func() {
			Eventually(komega.Update(cpms, func() {
				cpms.Spec.Selector.MatchLabels["new"] = "value"
			})).Should(MatchError(ContainSubstring("Forbidden: control plane machine set selector is immutable")), "The selector should be immutable")
		})
	})
})
