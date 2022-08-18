# Change Log

All notable changes to this project will be documented in this file.
 
The format is based on [Keep a Changelog](http://keepachangelog.com/)
and this project adheres to [Semantic Versioning](http://semver.org/).

## [v0.3.0](https://github.com/ray-project/kuberay/tree/v0.3.0) (2022-08-17)

### RayService (new feature!)

* [rayservice] Fix config names to match serve config format directly ([#464](https://github.com/ray-project/kuberay/pull/464), @edoakes)
* Disable pin on head for serve controller by default in service operator ([#457](https://github.com/ray-project/kuberay/pull/457), @iycheng)
* add wget timeout to probes ([#448](https://github.com/ray-project/kuberay/pull/448), @wilsonwang371)
* Disable async serve handler in Ray Service cluster. ([#447](https://github.com/ray-project/kuberay/pull/447), @iycheng)
* Add more env for RayService head or worker pods ([#439](https://github.com/ray-project/kuberay/pull/439), @brucez-anyscale)
* RayCluster created by RayService set death info env for ray container ([#419](https://github.com/ray-project/kuberay/pull/419), @brucez-anyscale)
* Add integration test for kuberay ray service and improve ray service operator ([#415](https://github.com/ray-project/kuberay/pull/415), @brucez-anyscale)
* Fix a potential reconcile issue for RayService and allow config unhealth time threshold in CR ([#384](https://github.com/ray-project/kuberay/pull/384), @brucez-anyscale)
* [Serve] Unify logger and add user facing events ([#378](https://github.com/ray-project/kuberay/pull/378), @simon-mo)
* Improve RayService Operator logic to handle head node crash ([#376](https://github.com/ray-project/kuberay/pull/376), @brucez-anyscale)
* Add serving service for users traffic with health check ([#367](https://github.com/ray-project/kuberay/pull/367), @brucez-anyscale)
* Create a service for dashboard agent ([#324](https://github.com/ray-project/kuberay/pull/324), @brucez-anyscale)
* Update RayService CR to integrate with Ray Nightly ([#322](https://github.com/ray-project/kuberay/pull/322), @brucez-anyscale)
* RayService: zero downtime update and healthcheck HA recovery ([#307](https://github.com/ray-project/kuberay/pull/307), @brucez-anyscale)
* RayService: Dev RayService CR and Controller logic ([#287](https://github.com/ray-project/kuberay/pull/287), @brucez-anyscale)
* KubeRay: kubebuilder creat RayService Controller and CR ([#270](https://github.com/ray-project/kuberay/pull/270), @brucez-anyscale)

### RayJob (new feature!)

* Properly convert unix time into meta time ([#480](https://github.com/ray-project/kuberay/pull/480), @pingsutw)
* Fix nil pointer dereference ([#429](https://github.com/ray-project/kuberay/pull/429), @pingsutw)
* Improve RayJob controller quality to alpha ([#398](https://github.com/ray-project/kuberay/pull/398), @Jeffwan)
* Submit ray job after cluster is ready ([#405](https://github.com/ray-project/kuberay/pull/405), @pingsutw)
* Add RayJob CRD and controller logic ([#303](https://github.com/ray-project/kuberay/pull/303), @harryge00)

### Cluster Fault Tolerant (new feature!)

* tune readiness probe timeouts ([#411](https://github.com/ray-project/kuberay/pull/411), @wilsonwang371)
* enable ray external storage namespace ([#406](https://github.com/ray-project/kuberay/pull/406), @wilsonwang371)
* Initial support for external Redis and GCS HA ([#294](https://github.com/ray-project/kuberay/pull/294), @wilsonwang371)

### Autoscaler (new feature!)

* [Autoscaler] Match autoscaler image to Ray head image for Ray >= 2.0.0 ([#423](https://github.com/ray-project/kuberay/pull/423), @DmitriGekhtman)
* [autoscaler] Better defaults and config options ([#414](https://github.com/ray-project/kuberay/pull/414), @DmitriGekhtman)
* [autoscaler] Make log file mount path more specific. ([#391](https://github.com/ray-project/kuberay/pull/391), @DmitriGekhtman)
* [autoscaler] Flip prioritize-workers-to-delete feature flag ([#379](https://github.com/ray-project/kuberay/pull/379), @DmitriGekhtman)
* Update autoscaler image ([#371](https://github.com/ray-project/kuberay/pull/371), @DmitriGekhtman)
* [minor] Update autoscaler image. ([#313](https://github.com/ray-project/kuberay/pull/313), @DmitriGekhtman)
* Provide override for autoscaler image pull policy. ([#297](https://github.com/ray-project/kuberay/pull/297), @DmitriGekhtman)
* [RFC][autoscaler] Add autoscaler container overrides and config options for scale behavior. ([#278](https://github.com/ray-project/kuberay/pull/278), @DmitriGekhtman)
* [autoscaler] Improve autoscaler auto-configuration, upstream recent improvements to Kuberay NodeProvider ([#274](https://github.com/ray-project/kuberay/pull/274), @DmitriGekhtman)

### Operator

* correct gcs ha to gcs ft ([#482](https://github.com/ray-project/kuberay/pull/482), @wilsonwang371)
* Fix panic in cleanupInvalidVolumeMounts  ([#481](https://github.com/ray-project/kuberay/pull/481), @MissiontoMars)
* fix: worker node can't connect to head node service ([#445](https://github.com/ray-project/kuberay/pull/445), @pingsutw)
* Add http resp code check for kuberay ([#435](https://github.com/ray-project/kuberay/pull/435), @brucez-anyscale)
* Fix wrong ray start command ([#431](https://github.com/ray-project/kuberay/pull/431), @pingsutw)
* fix controller: use Service's TargetPort ([#383](https://github.com/ray-project/kuberay/pull/383), @davidxia)
* Generate clientset for new specs ([#392](https://github.com/ray-project/kuberay/pull/392), @Basasuya)
* Add Ray address env. ([#388](https://github.com/ray-project/kuberay/pull/388), @DmitriGekhtman)
* Add the support to replace evicted head pod ([#381](https://github.com/ray-project/kuberay/pull/381), @Jeffwan)
* [Bug] Fix raycluster updatestatus list wrong label ([#377](https://github.com/ray-project/kuberay/pull/377), @scarlet25151)
* Make replicas optional for the head spec. ([#362](https://github.com/ray-project/kuberay/pull/362), @DmitriGekhtman)
* Add ray head service endpoints in status for expose raycluster's head node endpoints ([#341](https://github.com/ray-project/kuberay/pull/341), @scarlet25151)
* Support KubeRay management labels ([#345](https://github.com/ray-project/kuberay/pull/345), @Jeffwan)
* fix: bug in object store memory validation ([#332](https://github.com/ray-project/kuberay/pull/332), @davidxia)
* feat: add EventReason type for events ([#334](https://github.com/ray-project/kuberay/pull/334), @davidxia)
* minor refactor: fix camel-casing of unHealthy -> unhealthy ([#333](https://github.com/ray-project/kuberay/pull/333), @davidxia)
* refactor: remove redundant imports ([#317](https://github.com/ray-project/kuberay/pull/317), @davidxia)
* Fix GPU-autofill for rayStartParams ([#328](https://github.com/ray-project/kuberay/pull/328), @DmitriGekhtman)
* ray-operator: add missing space in controller log messages ([#316](https://github.com/ray-project/kuberay/pull/316), @davidxia)
* fix: use head group's ServiceAccount in autoscaler RoleBinding ([#315](https://github.com/ray-project/kuberay/pull/315), @davidxia)
* fix typos in comments and help messages ([#304](https://github.com/ray-project/kuberay/pull/304), @davidxia)
* enable force cluster upgrade ([#231](https://github.com/ray-project/kuberay/pull/231), @wilsonwang371)
* fix operator: correctly set head pod service account ([#276](https://github.com/ray-project/kuberay/pull/276), @davidxia)
* [hotfix] Fix Service account typo ([#285](https://github.com/ray-project/kuberay/pull/285), @DmitriGekhtman)
* Rename RayCluster folder to Ray since the group is Ray ([#275](https://github.com/ray-project/kuberay/pull/275), @brucez-anyscale)
* KubeRay: Relocate files to enable controller extension with Kubebuilder ([#268](https://github.com/ray-project/kuberay/pull/268), @brucez-anyscale)
* fix: use configured RayCluster service account when autoscaling ([#259](https://github.com/ray-project/kuberay/pull/259), @davidxia)
* suppress not found errors into regular logs ([#222](https://github.com/ray-project/kuberay/pull/222), @akanso)
* adding label check ([#221](https://github.com/ray-project/kuberay/pull/221), @akanso)
* Prioritize WorkersToDelete ([#208](https://github.com/ray-project/kuberay/pull/208), @sriram-anyscale)
* Simplify k8s client creation ([#179](https://github.com/ray-project/kuberay/pull/179), @chenk008)
* [ray-operator]Make log timestamp readable ([#206](https://github.com/ray-project/kuberay/pull/206), @chenk008)
* bump controller-runtime to 0.11.1 and  Kubernetes to v1.23 ([#180](https://github.com/ray-project/kuberay/pull/180), @chenk008)

### APIServer

* Add envs in cluster service api ([#432](https://github.com/ray-project/kuberay/pull/432), @MissiontoMars)
* Expose swallowed detail error messages ([#422](https://github.com/ray-project/kuberay/pull/422), @Jeffwan)
* fix: typo RAY_DISABLE_DOCKER_CPU_WRARNING -> RAY_DISABLE_DOCKER_CPU_WARNING ([#421](https://github.com/ray-project/kuberay/pull/421), @pingsutw)
* Add hostPathType and mountPropagationMode field for apiserver ([#413](https://github.com/ray-project/kuberay/pull/413), @scarlet25151)
* Fix `ListAllComputeTemplates` proto comments ([#407](https://github.com/ray-project/kuberay/pull/407), @MissiontoMars)
* Enable DefaultHTTPErrorHandler and Upgrade grpc-gateway to v2 ([#369](https://github.com/ray-project/kuberay/pull/369), @Jeffwan)
* Validate namespace consistency in the request when creating the cluster and the compute template  ([#365](https://github.com/ray-project/kuberay/pull/365), @daikeshi)
* Update compute template service url to include namespace path param ([#363](https://github.com/ray-project/kuberay/pull/363), @Jeffwan)
* fix apiserver created raycluster metrics port missing and check ([#356](https://github.com/ray-project/kuberay/pull/356), @scarlet25151)
* Support mounting volumes in API request ([#346](https://github.com/ray-project/kuberay/pull/346), @Jeffwan)
* add standard label for the filtering of cluster ([#342](https://github.com/ray-project/kuberay/pull/342), @scarlet25151)
* expose kubernetes events in apiserver ([#343](https://github.com/ray-project/kuberay/pull/343), @scarlet25151)
* Update ray-operator version in the apiserver ([#340](https://github.com/ray-project/kuberay/pull/340), @pingsutw)
* fix: typo worker_group_sepc -> worker_group_spec ([#330](https://github.com/ray-project/kuberay/pull/330), @davidxia)
* Fix gpu-accelerator in template ([#296](https://github.com/ray-project/kuberay/pull/296), @armandpicard)
* Add namespace scope to compute template operations ([#244](https://github.com/ray-project/kuberay/pull/244), @daikeshi)
* Add namespace scope to list operation ([#237](https://github.com/ray-project/kuberay/pull/237), @daikeshi)
* Add namespace scope for Ray cluster get and delete operations  ([#229](https://github.com/ray-project/kuberay/pull/229), @daikeshi)

### CLI

* Cli: make namespace optional to adapt to ListAll operation ([#361](https://github.com/ray-project/kuberay/pull/361), @Jeffwan)

### Deployment (kubernetes & helm)

* sync up helm chart's role ([#472](https://github.com/ray-project/kuberay/pull/472), @scarlet25151)
* helm-charts/ray-cluster: Allow extra workers ([#451](https://github.com/ray-project/kuberay/pull/451), @ulfox)
* Update helm chart version to 0.3.0 ([#461](https://github.com/ray-project/kuberay/pull/461), @Jeffwan)
* helm-chart/ray-cluster: allow head autoscaling ([#443](https://github.com/ray-project/kuberay/pull/443), @ulfox)
* modify kuberay operator crds in kuberay operator chart and add apiserver chart ([#354](https://github.com/ray-project/kuberay/pull/354), @scarlet25151)
* Warn explicitly against using kubectl apply to create RayCluster CRD. ([#302](https://github.com/ray-project/kuberay/pull/302), @DmitriGekhtman)
* Sync crds to Helm chart ([#280](https://github.com/ray-project/kuberay/pull/280), @haoxins)
* [Feature]Run kuberay in a single namespace ([#258](https://github.com/ray-project/kuberay/pull/258), @wilsonwang371)
* fix duplicated port config and manager.yaml missing config ([#250](https://github.com/ray-project/kuberay/pull/250), @wilsonwang371)
* manifests: Add live/ready probes ([#243](https://github.com/ray-project/kuberay/pull/243), @haoxins)
* Helm: supports custom probe seconds ([#239](https://github.com/ray-project/kuberay/pull/239), @haoxins)
* Add CD for helm charts ([#199](https://github.com/ray-project/kuberay/pull/199), @ddelange)

### Build and Testing

* Enable docker image push for release-0.3 branch ([#462](https://github.com/ray-project/kuberay/pull/462), @Jeffwan)
* add new 8000 port forwarding in kind ([#424](https://github.com/ray-project/kuberay/pull/424), @wilsonwang371)
* improve compatibility test stability ([#418](https://github.com/ray-project/kuberay/pull/418), @wilsonwang371)
* improve test stability ([#394](https://github.com/ray-project/kuberay/pull/394), @wilsonwang371)
* use more strict formatting ([#385](https://github.com/ray-project/kuberay/pull/385), @wilsonwang371)
* fix flaky test issue ([#370](https://github.com/ray-project/kuberay/pull/370), @wilsonwang371)
* provide more detailed information in case of test failures ([#352](https://github.com/ray-project/kuberay/pull/352), @wilsonwang371)
* fix wrong kuberay image used by compatibility test ([#327](https://github.com/ray-project/kuberay/pull/327), @wilsonwang371)
* add cluster nodes info test ([#299](https://github.com/ray-project/kuberay/pull/299), @wilsonwang371)
* Fix the image name in deploy cmd ([#293](https://github.com/ray-project/kuberay/pull/293), @brucez-anyscale)
* [CI]enable ci test to check ctrl plane health state ([#279](https://github.com/ray-project/kuberay/pull/279), @wilsonwang371)
* [bugfix]update flaky test timeout ([#254](https://github.com/ray-project/kuberay/pull/254), @wilsonwang371)
* Update format by running gofumpt ([#236](https://github.com/ray-project/kuberay/pull/236), @wilsonwang371)
* Add unit tests for raycluster_controller reconcilePods function ([#219](https://github.com/ray-project/kuberay/pull/219), @Waynegates)
* Support ray 1.12 ([#245](https://github.com/ray-project/kuberay/pull/245), @wilsonwang371)
* add 1.11 to compatibility test and update comment ([#217](https://github.com/ray-project/kuberay/pull/217), @wilsonwang371)
* run compatibility in parallel using multiple workflows ([#215](https://github.com/ray-project/kuberay/pull/215), @wilsonwang371)

### Monitoring

* add-state-machine-and-exposing-port ([#319](https://github.com/ray-project/kuberay/pull/319), @scarlet25151)
* Install: Fix directory path for prometheus install.sh ([#256](https://github.com/ray-project/kuberay/pull/256), @Tomcli)
* Fix Ray Operator prometheus config ([#253](https://github.com/ray-project/kuberay/pull/253), @Tomcli)
* Emit prometheus metrics from kuberay control plane ([#232](https://github.com/ray-project/kuberay/pull/232), @Jeffwan)
* Enable metrics-export-port by default and configure prometheus monitoring ([#230](https://github.com/ray-project/kuberay/pull/230), @scarlet25151)

### Docs

* [doc] Config and doc updates ahead of KubeRay 0.3.0/Ray 2.0.0 ([#486](https://github.com/ray-project/kuberay/pull/486), @DmitriGekhtman)
* document the raycluster status ([#473](https://github.com/ray-project/kuberay/pull/473), @scarlet25151)
* Clean up example samples ([#434](https://github.com/ray-project/kuberay/pull/434), @DmitriGekhtman)
* Add ray state api doc link in ray service doc ([#428](https://github.com/ray-project/kuberay/pull/428), @brucez-anyscale)
* [docs] Add sample configs with larger Ray pods ([#426](https://github.com/ray-project/kuberay/pull/426), @DmitriGekhtman)
* Add RayJob docs and development docs ([#404](https://github.com/ray-project/kuberay/pull/404), @Jeffwan)
* Add gcs ha doc into mkdocs ([#402](https://github.com/ray-project/kuberay/pull/402), @brucez-anyscale)
* [minor] Add client and dashboard ports to ports in example configs. ([#399](https://github.com/ray-project/kuberay/pull/399), @DmitriGekhtman)
* Add documentation for RayService ([#387](https://github.com/ray-project/kuberay/pull/387), @brucez-anyscale)
* Fix broken links by creating referenced soft links ([#335](https://github.com/ray-project/kuberay/pull/335), @Jeffwan)
* Support hosting swagger ui in apiserver ([#344](https://github.com/ray-project/kuberay/pull/344), @Jeffwan)
* Remove autoscaler debug example to prevent confusion ([#326](https://github.com/ray-project/kuberay/pull/326), @Jeffwan)
* Add a link to protobuf-grpc-service design page in proto doc ([#310](https://github.com/ray-project/kuberay/pull/310), @yabuchan)
* update readme and address issue #286 ([#311](https://github.com/ray-project/kuberay/pull/311), @wilsonwang371)
* docs fix: specify only Go 1.16 or 1.17 works right now ([#261](https://github.com/ray-project/kuberay/pull/261), @davidxia)
* Add documention link in readme ([#247](https://github.com/ray-project/kuberay/pull/247), @simon-mo)
* Use mhausenblas/mkdocs-deploy-gh-pages action for docs ([#233](https://github.com/ray-project/kuberay/pull/233), @Jeffwan)
* Build KubeRay Github site ([#216](https://github.com/ray-project/kuberay/pull/216), @Jeffwan)


## [v0.2.0](https://github.com/ray-project/kuberay/tree/v0.2.0) (2022-03-13)

### Features

* Support envFrom in rayclusters deployed with Helm ([#183](https://github.com/ray-project/kuberay/pull/183), @ebr)
* Helm: support imagePullSecrets for ray clusters ([#182](https://github.com/ray-project/kuberay/pull/182), @ebr)
* Support scheduling constraints in Helm-deployed clusters ([#181](https://github.com/ray-project/kuberay/pull/181), @ebr)
* Helm: ensure RBAC rules are up to date with the latest autogenerated manifest ([#175](https://github.com/ray-project/kuberay/pull/175), @ebr)
* add resource command ([#170](https://github.com/ray-project/kuberay/pull/170), @zhuangzhuang131419)
* Use container to generate proto files ([#160](https://github.com/ray-project/kuberay/pull/160), @Jeffwan)
* Support in-tree autoscaler ([#163](https://github.com/ray-project/kuberay/pull/163), @Jeffwan)
* [CLI] check viper error ([#172](https://github.com/ray-project/kuberay/pull/172), @chenk008)
* [Feature]Add subcommand `--version` ([#166](https://github.com/ray-project/kuberay/pull/166), @chenk008)
* [Feature] Add flag `watch-namespace` ([#165](https://github.com/ray-project/kuberay/pull/165), @chenk008)
* Support enableIngress for RayCluster ([#38](https://github.com/ray-project/kuberay/pull/38), @Jeffwan)
* Add CRD verb permission in helm ([#144](https://github.com/ray-project/kuberay/pull/144), @chenk008)
* Add quick start deployment manifests ([#132](https://github.com/ray-project/kuberay/pull/132), @Jeffwan)
* Add CLI to kuberay ([#135](https://github.com/ray-project/kuberay/pull/135), @wolfsniper2388)
* Ray Operator: Upgrade to Go v1.17 ([#128](https://github.com/ray-project/kuberay/pull/128), @haoxins)
* Add deploy manifests for apiserver ([#119](https://github.com/ray-project/kuberay/pull/119), @Jeffwan)
* Implement resource manager and gRPC services ([#127](https://github.com/ray-project/kuberay/pull/127), @Jeffwan)
* Generate go clients and swagger files ([#126](https://github.com/ray-project/kuberay/pull/126), @Jeffwan)
* [service] Init backend service project ([#113](https://github.com/ray-project/kuberay/pull/113), @Jeffwan)
* Add gRPC service definition and gRPC gateway ([#112](https://github.com/ray-project/kuberay/pull/112), @Jeffwan)
* [proto] Add core api definitions as protobuf message ([#93](https://github.com/ray-project/kuberay/pull/93), @Jeffwan)
* Use ray start block in Pod's entrypoint ([#77](https://github.com/ray-project/kuberay/pull/77), @chenk008)
* Add generated clientsets, informers and listers ([#97](https://github.com/ray-project/kuberay/pull/97), @Jeffwan)
* Add codegen scripts and make required api changes ([#96](https://github.com/ray-project/kuberay/pull/96), @harryge00)
* Reorganize api folder for code generation ([#91](https://github.com/ray-project/kuberay/pull/91), @harryge00)

### Bug fixes

* Fix serviceaccount typo in operator role ([#188](https://github.com/ray-project/kuberay/pull/188), @Jeffwan)
* Fix cli typo ([#173](https://github.com/ray-project/kuberay/pull/173), @chenk008)
* [Bug]Leader election need lease permission ([#169](https://github.com/ray-project/kuberay/pull/169), @chenk008)
* refactor: rename kubray -> kuberay ([#145](https://github.com/ray-project/kuberay/pull/145), @tekumara)
* Fix the Helm chart's image name ([#130](https://github.com/ray-project/kuberay/pull/130), @haoxins)
* fix typo in the helm chart templates ([#129](https://github.com/ray-project/kuberay/pull/129), @haoxins)
* fix issue that modifies the list while iterating through it ([#125](https://github.com/ray-project/kuberay/pull/125), @wilsonwang371)
* Add helm ([#109](https://github.com/ray-project/kuberay/pull/109), @zhuangzhuang131419)
* Update samples yaml ([#102](https://github.com/ray-project/kuberay/pull/102), @ryantd)
* fix missing template objectmeta ([#95](https://github.com/ray-project/kuberay/pull/95), @chenk008)
* fix typo in Readme ([#81](https://github.com/ray-project/kuberay/pull/81), @denkensk)

### Testing

* kuberay compatibility test with ray ([#157](https://github.com/ray-project/kuberay/pull/157), @wilsonwang371)
* Setup ci for apiserver ([#162](https://github.com/ray-project/kuberay/pull/162), @Jeffwan)
* Enable gofmt and move goimports to linter job ([#158](https://github.com/ray-project/kuberay/pull/158), @Jeffwan)
* add more debug info for bug-150: goimport issue ([#151](https://github.com/ray-project/kuberay/pull/151), @wilsonwang371)
* add nightly docker build workflow ([#141](https://github.com/ray-project/kuberay/pull/141), @wilsonwang371)
* enable goimport and add new makefile target to only build image without test ([#123](https://github.com/ray-project/kuberay/pull/123), @wilsonwang371)
* [Feature]add docker build stage to ci workflow ([#122](https://github.com/ray-project/kuberay/pull/122), @wilsonwang371)
* Pass --timeout option to golangci-lint ([#116](https://github.com/ray-project/kuberay/pull/116), @Jeffwan)
* Add linter job for github workflow ([#79](https://github.com/ray-project/kuberay/pull/79), @feilengcui008)

### Docs and Miscs

* Add Makefile for cli project ([#192](https://github.com/ray-project/kuberay/pull/192), @Jeffwan)
* Manifests and docs improvement for prerelease  ([#191](https://github.com/ray-project/kuberay/pull/191), @Jeffwan)
* Add documentation for autoscaling feature ([#189](https://github.com/ray-project/kuberay/pull/189), @Jeffwan)
* docs: Fix typo in best practice ([#190](https://github.com/ray-project/kuberay/pull/190), @nakamasato)
* add kuberay on kind jupyter notebook ([#147](https://github.com/ray-project/kuberay/pull/147), @wilsonwang371)
* Add KubeRay release guideline ([#161](https://github.com/ray-project/kuberay/pull/161), @Jeffwan)
* Add troubleshooting guide for ray version mismatch ([#154](https://github.com/ray-project/kuberay/pull/154), @scarlet25151)
* Explanation and Best Practice for workers-head Reconnection ([#142](https://github.com/ray-project/kuberay/pull/142), @nostalgicimp)
* [docs] Folder name change to kuberay-operator ([#143](https://github.com/ray-project/kuberay/pull/143), @asm582)
* Improve the Helm charts docs ([#131](https://github.com/ray-project/kuberay/pull/131), @haoxins)
* add auto-scale doc ([#108](https://github.com/ray-project/kuberay/pull/108), @akanso)
* Add core API and backend service design doc ([#98](https://github.com/ray-project/kuberay/pull/98), @Jeffwan)
* [Feature] add more options in bug template ([#121](https://github.com/ray-project/kuberay/pull/121), @wilsonwang371)
* Rename service module to apiserver ([#118](https://github.com/ray-project/kuberay/pull/118), @Jeffwan)


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
