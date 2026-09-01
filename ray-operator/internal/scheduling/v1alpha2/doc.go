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

// Package v1alpha2 is a frozen, vendored copy of the upstream
// k8s.io/api/scheduling/v1alpha2 types (as of Kubernetes 1.36). The upstream
// package is removed in k8s.io/api v0.37+, so KubeRay keeps this local copy to
// continue supporting scheduling.k8s.io/v1alpha2 on 1.36 clusters after the
// operator's k8s.io/api dependency is bumped past 1.36. Do not add features
// here; newer functionality belongs in the v1beta1/v1alpha3 providers.
//
// These are built-in Kubernetes API types (served by the apiserver behind the
// GenericWorkload feature gate), not KubeRay CRDs. The upstream code-generation
// markers (e.g. +groupName, +genclient, +k8s:deepcopy-gen) are intentionally
// omitted, and +kubebuilder:skip tells controller-gen not to emit CRDs or
// regenerate clients for this package.
//
// +kubebuilder:skip
package v1alpha2
