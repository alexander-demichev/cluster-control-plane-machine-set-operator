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

package resourcebuilder

import (
	machinev1 "github.com/openshift/api/machine/v1"
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// controlPlaneMachineSetName is the only valid name allowed.
	// A ControlPlaneMachineSet is a singleton within the cluster, this matches other singletons such as Infrastructure.
	controlPlaneMachineSetName = "cluster"
)

// ControlPlaneMachineSet creates a new controlplanemachineset builder.
func ControlPlaneMachineSet() ControlPlaneMachineSetBuilder {
	return ControlPlaneMachineSetBuilder{
		machineTemplateBuilder: OpenShiftMachineV1Beta1Template(),
		name:                   controlPlaneMachineSetName,
		namespace:              openshiftMachineAPINamespaceName,
		replicas:               3,
		selector: metav1.LabelSelector{
			MatchLabels: map[string]string{
				machineRoleLabelName:                 "master",
				machineTypeLabelName:                 "master",
				machinev1beta1.MachineClusterIDLabel: "cpms-cluster-test-id",
			},
		},
		strategyType: machinev1.RollingUpdate,
	}
}

// ControlPlaneMachineSetBuilder is used to build out a controlplanemachineset object.
type ControlPlaneMachineSetBuilder struct {
	generation             int64
	machineTemplateBuilder ControlPlaneMachineSetTemplateBuilder
	name                   string
	namespace              string
	replicas               int32
	selector               metav1.LabelSelector
	strategyType           machinev1.ControlPlaneMachineSetStrategyType
	conditions             []metav1.Condition
}

// Build builds a new controlplanemachineset based on the configuration provided.
func (m ControlPlaneMachineSetBuilder) Build() *machinev1.ControlPlaneMachineSet {
	cpms := &machinev1.ControlPlaneMachineSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:       m.name,
			Namespace:  m.namespace,
			Generation: m.generation,
		},
		Spec: machinev1.ControlPlaneMachineSetSpec{
			Replicas: int32Ptr(m.replicas),
			Selector: m.selector,
			Strategy: machinev1.ControlPlaneMachineSetStrategy{
				Type: m.strategyType,
			},
		},
		Status: machinev1.ControlPlaneMachineSetStatus{
			Conditions: m.conditions,
		},
	}

	if m.machineTemplateBuilder != nil {
		cpms.Spec.Template = m.machineTemplateBuilder.BuildTemplate()
	}

	return cpms
}

// WithMachineTemplateBuilder sets the machine template builder for the controlplanemachineset builder.
func (m ControlPlaneMachineSetBuilder) WithMachineTemplateBuilder(builder ControlPlaneMachineSetTemplateBuilder) ControlPlaneMachineSetBuilder {
	m.machineTemplateBuilder = builder
	return m
}

// WithName sets the name for the controlplanemachineset builder.
func (m ControlPlaneMachineSetBuilder) WithName(name string) ControlPlaneMachineSetBuilder {
	m.name = name
	return m
}

// WithNamespace sets the namespace for the controlplanemachineset builder.
func (m ControlPlaneMachineSetBuilder) WithNamespace(namespace string) ControlPlaneMachineSetBuilder {
	m.namespace = namespace
	return m
}

// WithGeneration sets the generation for the controlerplanemachineset builder.
func (m ControlPlaneMachineSetBuilder) WithGeneration(generation int64) ControlPlaneMachineSetBuilder {
	m.generation = generation
	return m
}

// WithReplicas sets the replicas for the controlplanemachineset builder.
func (m ControlPlaneMachineSetBuilder) WithReplicas(replicas int32) ControlPlaneMachineSetBuilder {
	m.replicas = replicas
	return m
}

// WithSelector sets the selector for the controlplanemachineset builder.
func (m ControlPlaneMachineSetBuilder) WithSelector(selector metav1.LabelSelector) ControlPlaneMachineSetBuilder {
	m.selector = selector
	return m
}

// WithStrategyType sets the update strategy type for the controlplanemachineset builder.
func (m ControlPlaneMachineSetBuilder) WithStrategyType(strategy machinev1.ControlPlaneMachineSetStrategyType) ControlPlaneMachineSetBuilder {
	m.strategyType = strategy
	return m
}

// WithConditions sets the conditions for the controlplanemachineset builder.
func (m ControlPlaneMachineSetBuilder) WithConditions(conditions []metav1.Condition) ControlPlaneMachineSetBuilder {
	m.conditions = conditions
	return m
}
