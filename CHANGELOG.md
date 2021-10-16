# Change Log

All notable changes to this project will be documented in this file.
 
The format is based on [Keep a Changelog](http://keepachangelog.com/)
and this project adheres to [Semantic Versioning](http://semver.org/).

## [v0.1.0](https://github.com/ray-project/kuberay/tree/v0.1.0) (2021-10-16)

### Feature

* Check duplicate services explicitly ([#72](https://github.com/ray-project/kuberay/pull/72), @Jeffwan)
* Expose reconcile concurrency as a command flag ([#67](https://github.com/ray-project/kuberay/pull/67), @feilengcui008)
* Ignore reconcile cluster being deleted ([#63](https://github.com/ray-project/kuberay/pull/63), @feilengcui008)
* Add issue and pr templates ([#44](https://github.com/ray-project/kuberay/pull/44), @chaomengyuan)
* Create root level .gitignore file ([#37](https://github.com/ray-project/kuberay/pull/37), @Jeffwan)
* Remove BAZEL build in ray-operator project ([#32](https://github.com/ray-project/kuberay/pull/32), @chenk008)
* Upgrade Kubebuilder to 3.0.0 and optimize Github workflow ([#31](https://github.com/ray-project/kuberay/pull/31), @Jeffwan)
* Update v1alpha1 RayCluster CRD and controllers ([#22](https://github.com/ray-project/kuberay/pull/22), @Jeffwan)
* Deprecate msft operator and rename to ray-operator ([#20](https://github.com/ray-project/kuberay/pull/20), @Jeffwan)
* Deprecate ByteDance operator and move to unified one ([#19](https://github.com/ray-project/kuberay/pull/19), @Jeffwan)
* Deprecate antgroup ray operator and move to unified implementation ([#18](https://github.com/ray-project/kuberay/pull/18), @chenk008)
* Upgrade to go 1.15 ([#12](https://github.com/ray-project/kuberay/pull/12), @tgaddair)
* Remove unused generated manifest from kubebuilder ([#11](https://github.com/ray-project/kuberay/pull/11), @Jeffwan)
* Clean up kustomization manifests ([#10](https://github.com/ray-project/kuberay/pull/10), @Jeffwan)
* Add RayCluster v1alpha1 controller ([#8](https://github.com/ray-project/kuberay/pull/8), @Jeffwan)
* Scaffolding out Bytedance's ray operator project  ([#7](https://github.com/ray-project/kuberay/pull/7), @Jeffwan)
* allow deletion of workers ([#5](https://github.com/ray-project/kuberay/pull/5), @akanso)
* Ray Autoscaler integrate with Ray K8s Operator ([#2](https://github.com/ray-project/kuberay/pull/2), @Qstar)
* Add license ([#3](https://github.com/ray-project/kuberay/pull/3), @akanso)
* Operator with Design 1B ([#1](https://github.com/ray-project/kuberay/pull/1), @akanso)

### Bugs

* Fix flaky tests by retrying 409 conflict error ([#73](https://github.com/ray-project/kuberay/pull/73), @Jeffwan)
* Fix issues in heterogeneous sample ([#45](https://github.com/ray-project/kuberay/pull/45), @anencore94)
* Fix incorrect manifest setting and remove unused manifests ([#34](https://github.com/ray-project/kuberay/pull/34), @Jeffwan)
* Fix status update issue and redis port formatting issue ([#16](https://github.com/ray-project/kuberay/pull/16), @Jeffwan)
* Fix leader election failure and crd too long issue ([#9](https://github.com/ray-project/kuberay/pull/9), @Jeffwan)

### Misc

* chore: Add github workflow and go report badge ([#58](https://github.com/ray-project/kuberay/pull/58), @Jeffwan)
* Update README.md ([#52](https://github.com/ray-project/kuberay/pull/52), @Jeffwan)
* Add community profile documentation ([#49](https://github.com/ray-project/kuberay/pull/49), @Jeffwan)
* Rename go module to kuberay ([#50](https://github.com/ray-project/kuberay/pull/50), @Jeffwan)
