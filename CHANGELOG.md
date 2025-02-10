# Change Log

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](http://keepachangelog.com/)
and this project adheres to [Semantic Versioning](http://semver.org/).

## v1.0.0 (2023-11-06)

### KubeRay is officially in General Availability!

* Bump the CRD version from v1alpha1 to v1.
* Relocate almost all documentation to the Ray website.
* Improve RayJob UX.
* Improve GCS fault tolerance.

### GCS fault tolerance

* [GCS FT] Improve GCS FT cleanup UX ([#1592](https://github.com/ray-project/kuberay/pull/1592), @kevin85421)
* [Bug][RayCluster] Fix RAY_REDIS_ADDRESS parsing with redis scheme and… ([#1556](https://github.com/ray-project/kuberay/pull/1556), @rueian)
* [Bug] RayService with GCS FT HA issue ([#1551](https://github.com/ray-project/kuberay/pull/1551), @kevin85421)
* [Test][GCS FT] End-to-end test for cleanup_redis_storage (#1422)(#1459) ([#1466](https://github.com/ray-project/kuberay/pull/1466), @rueian)
* [Feature][GCS FT] Clean up Redis once a GCS FT-Enabled RayCluster is deleted ([#1412](https://github.com/ray-project/kuberay/pull/1412), @kevin85421)
* Update GCS fault tolerance YAML ([#1404](https://github.com/ray-project/kuberay/pull/1404), @kevin85421)
* [GCS FT] Consider the case of sidecar containers ([#1386](https://github.com/ray-project/kuberay/pull/1386), @kevin85421)
* [GCS FT] Give readiness / liveness probes good default values ([#1364](https://github.com/ray-project/kuberay/pull/1364), @kevin85421)
* [GCS FT][Refactor] Redefine the behavior for deleting Pods and stop listening to Kubernetes events ([#1341](https://github.com/ray-project/kuberay/pull/1341), @kevin85421)

### CRD versioning

* [CRD] Inject CRD version to the Autoscaler sidecar container ([#1496](https://github.com/ray-project/kuberay/pull/1496), @kevin85421)
* [CRD][2/n] Update from CRD v1alpha1 to v1 ([#1482](https://github.com/ray-project/kuberay/pull/1482), @kevin85421)
* [CRD][1/n] Create v1 CRDs ([#1481](https://github.com/ray-project/kuberay/pull/1481), @kevin85421)
* [CRD] Set maxDescLen to 0 ([#1449](https://github.com/ray-project/kuberay/pull/1449), @kevin85421)

### RayService

* [Hotfix][Bug] Avoid unnecessary zero-downtime upgrade ([#1581](https://github.com/ray-project/kuberay/pull/1581), @kevin85421)
* [Feature] Add an example for RayService high availability ([#1566](https://github.com/ray-project/kuberay/pull/1566), @kevin85421)
* [Feature] Add a flag to make zero downtime upgrades optional ([#1564](https://github.com/ray-project/kuberay/pull/1564), @kevin85421)
* [Bug][RayService] KubeRay does not recreate Serve applications if a head Pod without GCS FT recovers from a failure. ([#1420](https://github.com/ray-project/kuberay/pull/1420), @kevin85421)
* [Bug] Fix the filename of text summarizer YAML ([#1415](https://github.com/ray-project/kuberay/pull/1415), @kevin85421)
* [serve] Change text ml yaml to use french in user config ([#1403](https://github.com/ray-project/kuberay/pull/1403), @zcin)
* [services] Add text ml rayservice yaml ([#1402](https://github.com/ray-project/kuberay/pull/1402), @zcin)
* [Bug] Fix flakiness of RayService e2e tests ([#1385](https://github.com/ray-project/kuberay/pull/1385), @kevin85421)
* Add RayService sample test ([#1377](https://github.com/ray-project/kuberay/pull/1377), @Darren221)
* [RayService] Revisit the conditions under which a RayService is considered unhealthy and the default threshold ([#1293](https://github.com/ray-project/kuberay/pull/1293), @kevin85421)
* [RayService][Observability] Add more loggings about networking issues ([#1282](https://github.com/ray-project/kuberay/pull/1282), @kevin85421)

### RayJob

* [Feature] Improve observability for flaky RayJob test ([#1587](https://github.com/ray-project/kuberay/pull/1587), @kevin85421)
* [Bug][RayJob] Fix FailedToGetJobStatus by allowing transition to Running ([#1583](https://github.com/ray-project/kuberay/pull/1583), @architkulkarni)
* [RayJob] Fix RayJob status reconciliation ([#1539](https://github.com/ray-project/kuberay/pull/1539), @astefanutti)
* [RayJob]: Always use target RayCluster image as default RayJob submitter image ([#1548](https://github.com/ray-project/kuberay/pull/1548), @astefanutti)
* [RayJob] Add default CPU and memory for job submitter pod ([#1319](https://github.com/ray-project/kuberay/pull/1319), @architkulkarni)
* [Bug][RayJob] Check dashboard readiness before creating job pod (#1381) ([#1429](https://github.com/ray-project/kuberay/pull/1429), @rueian)
* [Feature][RayJob] Use RayContainerIndex instead of 0 (#1397) ([#1427](https://github.com/ray-project/kuberay/pull/1427), @rueian)
* [RayJob] Enable job log streaming by setting `PYTHONUNBUFFERED` in job container ([#1375](https://github.com/ray-project/kuberay/pull/1375), @architkulkarni)
* Add field to expose entrypoint num cpus in rayjob ([#1359](https://github.com/ray-project/kuberay/pull/1359), @shubhscoder)
* [RayJob] Add runtime env YAML field ([#1338](https://github.com/ray-project/kuberay/pull/1338), @architkulkarni)
* [Bug][RayJob] RayJob with custom head service name ([#1332](https://github.com/ray-project/kuberay/pull/1332), @kevin85421)
* [RayJob] Add e2e sample yaml test for shutdownAfterJobFinishes ([#1269](https://github.com/ray-project/kuberay/pull/1269), @architkulkarni)

### RayCluster

* [Enhancement] Remove unused variables in constant.go ([#1474](https://github.com/ray-project/kuberay/pull/1474), @evalaiyc98)
* [Enhancement] GPU RayCluster doesn't work on GKE Autopilot ([#1470](https://github.com/ray-project/kuberay/pull/1470), @kevin85421)
* [Refactor] Parameterize TestGetAndCheckServeStatus ([#1450](https://github.com/ray-project/kuberay/pull/1450), @evalaiyc98)
* [Feature] Make replicas optional for WorkerGroupSpec ([#1443](https://github.com/ray-project/kuberay/pull/1443), @kevin85421)
* use raycluster app's name as podgroup name key word ([#1446](https://github.com/ray-project/kuberay/pull/1446), @lowang-bh)
* [Refactor] Make port name variables consistent and meaningful ([#1389](https://github.com/ray-project/kuberay/pull/1389), @evalaiyc98)
* [Feature] Use image of Ray head container as the default Ray Autoscaler container ([#1401](https://github.com/ray-project/kuberay/pull/1401), @kevin85421)
* Update Autoscaler YAML for the Autoscaler tutorial ([#1400](https://github.com/ray-project/kuberay/pull/1400), @kevin85421)
* [Feature] Ray container must be the first application container ([#1379](https://github.com/ray-project/kuberay/pull/1379), @kevin85421)
* [release blocker][Feature] Only Autoscaler can make decisions to delete Pods ([#1253](https://github.com/ray-project/kuberay/pull/1253), @kevin85421)
* [release blocker][Autoscaler] Randomly delete Pods when scaling down the cluster ([#1251](https://github.com/ray-project/kuberay/pull/1251), @kevin85421)

### Helm charts

* Remove miniReplicas in raycluster-cluster.yaml ([#1473](https://github.com/ray-project/kuberay/pull/1473), @evalaiyc98)
* Helm chart ray-cluster template reference fix ([#1469](https://github.com/ray-project/kuberay/pull/1469), @chrisxstyles)
* fix: Issue #1391 - Custom labels not being pulled in ([#1398](https://github.com/ray-project/kuberay/pull/1398), @rxraghu)
* Remove unnecessary kustomize in make helm ([#1370](https://github.com/ray-project/kuberay/pull/1370), @shubhscoder)
* [Feature] Allow RayCluster Helm chart to specify different images for different worker groups ([#1352](https://github.com/ray-project/kuberay/pull/1352), @Darren221)
* Allow manually creating init containers in Kuberay helm charts ([#1287](https://github.com/ray-project/kuberay/pull/1287), @richardsliu)

### KubeRay API Server

* Added Python API server client ([#1561](https://github.com/ray-project/kuberay/pull/1561), @blublinsky)
* updating url use v1 ([#1577](https://github.com/ray-project/kuberay/pull/1577), @blublinsky)
* Fixed processing of job submitter ([#1562](https://github.com/ray-project/kuberay/pull/1562), @blublinsky)
* extended job APIs ([#1537](https://github.com/ray-project/kuberay/pull/1537), @blublinsky)
* fixed volumes test in cluster test ([#1498](https://github.com/ray-project/kuberay/pull/1498), @blublinsky)
* Add documentation for API Server monitoring ([#1479](https://github.com/ray-project/kuberay/pull/1479), @blublinsky)
* created HA example for API server ([#1461](https://github.com/ray-project/kuberay/pull/1461), @blublinsky)
* Numerous fixes to the API server to make RayJob APIs working ([#1447](https://github.com/ray-project/kuberay/pull/1447), @blublinsky)
* Updated API server documentation ([#1435](https://github.com/ray-project/kuberay/pull/1435), @z103cb)
* servev2 support for API server ([#1419](https://github.com/ray-project/kuberay/pull/1419), @blublinsky)
* replacement for https://github.com/ray-project/kuberay/pull/1312 ([#1409](https://github.com/ray-project/kuberay/pull/1409), @blublinsky)
* Updates to the apiserver swagger-ui ([#1410](https://github.com/ray-project/kuberay/pull/1410), @z103cb)
* implemented liveness/readyness probe for the API server ([#1369](https://github.com/ray-project/kuberay/pull/1369), @blublinsky)
* Operator support for openShift ([#1371](https://github.com/ray-project/kuberay/pull/1371), @blublinsky)
* Removed use of the  of BUILD_FLAGS in apiserver makefile ([#1336](https://github.com/ray-project/kuberay/pull/1336), @z103cb)
* Api server makefile ([#1301](https://github.com/ray-project/kuberay/pull/1301), @z103cb)

### Documentation

* [Doc] Update release docs ([#1621](https://github.com/ray-project/kuberay/pull/1621), @kevin85421)
* [Doc] Fix release doc format ([#1578](https://github.com/ray-project/kuberay/pull/1578), @kevin85421)
* Update kuberay mcad integration doc ([#1373](https://github.com/ray-project/kuberay/pull/1373), @tedhtchang)
* [Release][Doc] Add instructions to release Go modules. ([#1546](https://github.com/ray-project/kuberay/pull/1546), @kevin85421)
* [Post v1.0.0-rc.1] Reenable sample YAML tests for latest release and update some docs ([#1544](https://github.com/ray-project/kuberay/pull/1544), @kevin85421)
* Update operator development instruction ([#1458](https://github.com/ray-project/kuberay/pull/1458), @tedhtchang)
* doc: fix moved link ([#1462](https://github.com/ray-project/kuberay/pull/1462), @hongchaodeng)
* Fix mkDocs ([#1448](https://github.com/ray-project/kuberay/pull/1448), @kevin85421)
* Update Kuberay doc to version 1.0.0 rc.0 ([#1441](https://github.com/ray-project/kuberay/pull/1441), @Yicheng-Lu-llll)
* [Doc] Delete unused docs ([#1440](https://github.com/ray-project/kuberay/pull/1440), @kevin85421)
* [Post Ray 2.7.0 Release] Update Ray versions to Ray 2.7.0 ([#1423](https://github.com/ray-project/kuberay/pull/1423), @GeneDer)
* [Doc] Update README ([#1433](https://github.com/ray-project/kuberay/pull/1433), @kevin85421)
* [release] Redirect users to Ray website ([#1431](https://github.com/ray-project/kuberay/pull/1431), @kevin85421)
* [Docs] Update Security Guidance on Dashboard Ingress ([#1413](https://github.com/ray-project/kuberay/pull/1413), @ijrsvt)
* Update Volcano integration doc ([#1380](https://github.com/ray-project/kuberay/pull/1380), @annajung)
* [Doc] Add gke bucket yaml ([#1372](https://github.com/ray-project/kuberay/pull/1372), @architkulkarni)
* [RayJob] [Doc] Add real-world Ray Job use case tutorial for KubeRay  ([#1361](https://github.com/ray-project/kuberay/pull/1361), @architkulkarni)
* Delete ray_v1alpha1_rayjob.batch-inference.yaml ([#1360](https://github.com/ray-project/kuberay/pull/1360), @architkulkarni)
* Documentation and example for running simple NLP service on kuberay ([#1340](https://github.com/ray-project/kuberay/pull/1340), @gvspraveen)
* Add a document for profiling ([#1299](https://github.com/ray-project/kuberay/pull/1299), @Yicheng-Lu-llll)
* Fix: Typo ([#1295](https://github.com/ray-project/kuberay/pull/1295), @ArgonQQ)
* [Post release v0.6.0] Update CHANGELOG.md ([#1274](https://github.com/ray-project/kuberay/pull/1274), @kevin85421)
* Release v0.6.0 doc validation ([#1271](https://github.com/ray-project/kuberay/pull/1271), @kevin85421)
* [Doc] Develop Ray Serve Python script on KubeRay ([#1250](https://github.com/ray-project/kuberay/pull/1250), @kevin85421)
* [Doc] Fix the order of comments in sample Job YAML file ([#1242](https://github.com/ray-project/kuberay/pull/1242), @architkulkarni)
* [Doc] Upload a screenshot for the Serve page in Ray dashboard ([#1236](https://github.com/ray-project/kuberay/pull/1236), @kevin85421)
* Fix typo ([#1241](https://github.com/ray-project/kuberay/pull/1241), @mmourafiq)

### CI

* [Bug] Fix flaky sample YAML tests ([#1590](https://github.com/ray-project/kuberay/pull/1590), @kevin85421)
* Allow to install and remove operator via scripts ([#1545](https://github.com/ray-project/kuberay/pull/1545), @jiripetrlik)
* [CI] Create release tag for ray-operator Go module ([#1574](https://github.com/ray-project/kuberay/pull/1574), @astefanutti)
* [Test][Bug] Update worker replias idempotently in rayjob autoscaler envtest (#1471) ([#1543](https://github.com/ray-project/kuberay/pull/1543), @rueian)
* Update Dockerfiles to address CVE-2023-44487 (HTTP/2 Rapid Reset) ([#1540](https://github.com/ray-project/kuberay/pull/1540), @astefanutti)
* [CI] Skip redis raycluster sample YAML test ([#1465](https://github.com/ray-project/kuberay/pull/1465), @architkulkarni)
* Revert "[CI] Skip redis raycluster sample YAML test" ([#1490](https://github.com/ray-project/kuberay/pull/1490), @rueian)
* Remove GOARCH in ray-operator/Dockfile to support multi-arch images ([#1442](https://github.com/ray-project/kuberay/pull/1442), @ideal)
* Update Dockerfile to address closed CVEs ([#1488](https://github.com/ray-project/kuberay/pull/1488), @anishasthana)
* [CI] Update latest release to v1.0.0-rc.0 in tests ([#1467](https://github.com/ray-project/kuberay/pull/1467), @architkulkarni)
* [CI] Reenable rayjob sample yaml latest test ([#1464](https://github.com/ray-project/kuberay/pull/1464), @architkulkarni)
* [CI] Skip redis raycluster sample YAML test ([#1465](https://github.com/ray-project/kuberay/pull/1465), @architkulkarni)
* Updating logrus and net packages in go.mod ([#1495](https://github.com/ray-project/kuberay/pull/1495), @jbusche)
* Allow E2E tests to run with arbitrary k8s cluster ([#1306](https://github.com/ray-project/kuberay/pull/1306), @jiripetrlik)
* Bump golang.org/x/net from 0.0.0-20210405180319-a5a99cb37ef4 to 0.7.0 in /proto ([#1345](https://github.com/ray-project/kuberay/pull/1345), @dependabot[bot])
* Bump golang.org/x/text from 0.3.5 to 0.3.8 in /proto ([#1344](https://github.com/ray-project/kuberay/pull/1344), @dependabot[bot])
* Bump go.mongodb.org/mongo-driver from 1.3.4 to 1.5.1 in /apiserver ([#1407](https://github.com/ray-project/kuberay/pull/1407), @dependabot[bot])
* Bump golang.org/x/sys from 0.0.0-20210510120138-977fb7262007 to 0.1.0 in /proto ([#1346](https://github.com/ray-project/kuberay/pull/1346), @dependabot[bot])
* Bump golang.org/x/net from 0.0.0-20210813160813-60bc85c4be6d to 0.7.0 in /cli ([#1405](https://github.com/ray-project/kuberay/pull/1405), @dependabot[bot])
* Bump github.com/emicklei/go-restful from 2.9.5+incompatible to 2.16.0+incompatible in /ray-operator ([#1348](https://github.com/ray-project/kuberay/pull/1348), @dependabot[bot])
* Bump golang.org/x/sys from 0.0.0-20211210111614-af8b64212486 to 0.1.0 in /cli ([#1347](https://github.com/ray-project/kuberay/pull/1347), @dependabot[bot])
* [CI] Remove RayService tests from comopatibility-test.py ([#1395](https://github.com/ray-project/kuberay/pull/1395), @kevin85421)
* [CI] Remove extraPortMappings from kind configurations ([#1366](https://github.com/ray-project/kuberay/pull/1366), @kevin85421)
* [CI] Update latest ray version 2.5.0 -> 2.6.3 ([#1320](https://github.com/ray-project/kuberay/pull/1320), @architkulkarni)
* Bump the golangci-lint version in the api server makefile ([#1342](https://github.com/ray-project/kuberay/pull/1342), @z103cb)
* [CI] Refactor pipeline and test RayCluster sample yamls ([#1321](https://github.com/ray-project/kuberay/pull/1321), @architkulkarni)
* Update doc and base image for Go 1.19 ([#1330](https://github.com/ray-project/kuberay/pull/1330), @tedhtchang)
* Fix release actions ([#1323](https://github.com/ray-project/kuberay/pull/1323), @anishasthana)
* Upgrade to Go 1.19 ([#1325](https://github.com/ray-project/kuberay/pull/1325), @kevin85421)
* [CI] Run sample job YAML tests in buildkite ([#1315](https://github.com/ray-project/kuberay/pull/1315), @architkulkarni)
* [CI] Downgrade `kind` from to `v0.20.0` to `v0.11.1` ([#1313](https://github.com/ray-project/kuberay/pull/1313), @architkulkarni)
* [CI] Publish KubeRay operator / apiserver images to Quay ([#1307](https://github.com/ray-project/kuberay/pull/1307), @kevin85421)
* [CI] Install kuberay operator in buildkite test ([#1308](https://github.com/ray-project/kuberay/pull/1308), @architkulkarni)
* [CI] Verify kubectl in kind-in-docker step ([#1305](https://github.com/ray-project/kuberay/pull/1305), @architkulkarni)
* [Quay] Sanity check for KubeRay repository setup ([#1300](https://github.com/ray-project/kuberay/pull/1300), @kevin85421)
* [CI] Only run test_ray_serve for Ray 2.6.0 and later ([#1288](https://github.com/ray-project/kuberay/pull/1288), @kevin85421)
* Update ray operator Dockerfile ([#1213](https://github.com/ray-project/kuberay/pull/1213), @anishasthana)
* [Golang] Remove `go get` ([#1283](https://github.com/ray-project/kuberay/pull/1283), @ijrsvt)
* Dependencies: Upgrade golang.org/x packages ([#1281](https://github.com/ray-project/kuberay/pull/1281), @ijrsvt)
* [CI] Add `kind`-in-Docker test to Buildkite CI ([#1243](https://github.com/ray-project/kuberay/pull/1243), @architkulkarni)

### Others

* Fix: odd number of arguments ([#1594](https://github.com/ray-project/kuberay/pull/1594), @chenk008)
* [Feature][Observability] Scrape Autoscaler and Dashboard metrics ([#1493](https://github.com/ray-project/kuberay/pull/1493), @kevin85421)
* [Benchmark] KubeRay memory / scalability benchmark ([#1324](https://github.com/ray-project/kuberay/pull/1324), @kevin85421)
* Do not update pod labels if they haven't changed ([#1304](https://github.com/ray-project/kuberay/pull/1304), @JoshKarpel)
* Add Ray cluster spec for TPU pods ([#1292](https://github.com/ray-project/kuberay/pull/1292), @richardsliu)
* [Grafana][Observability] Embed Grafana dashboard panels into Ray dashboard ([#1278](https://github.com/ray-project/kuberay/pull/1278), @kevin85421)
* [Feature] Allow custom labels&annotations for kuberay operator (#1275) ([#1276](https://github.com/ray-project/kuberay/pull/1276), @mariusp)

## v0.6.0 (2023-07-26)

### Highlights

* RayService
  * RayService starts to support Ray Serve multi-app API (#1136, #1156)
  * RayService stability improvements (#1231, #1207, #1173)
  * RayService observability (#1230)
  * RayService examples
    * [RayService] Stable Diffusion example ([#1181](https://github.com/ray-project/kuberay/pull/1181), @kevin85421)
    * MobileNet example ([#1175](https://github.com/ray-project/kuberay/pull/1175), @kevin85421)
  * RayService troubleshooting handbook (#1221)

* RayJob refactoring (#1177)
* Autoscaler stability improvements (#1251, #1253)

### RayService

* [RayService][Observability] Add more logging for RayService troubleshooting ([#1230](https://github.com/ray-project/kuberay/pull/1230), @kevin85421)
* [Bug] Long image pull time will trigger blue-green upgrade after the head is ready ([#1231](https://github.com/ray-project/kuberay/pull/1231), @kevin85421)
* [RayService] Stable Diffusion example ([#1181](https://github.com/ray-project/kuberay/pull/1181), @kevin85421)
* [RayService] Update docs to use multi-app ([#1179](https://github.com/ray-project/kuberay/pull/1179), @zcin)
* [RayService] Change runtime env for e2e autoscaling test ([#1178](https://github.com/ray-project/kuberay/pull/1178), @zcin)
* [RayService] Add e2e tests ([#1167](https://github.com/ray-project/kuberay/pull/1167), @zcin)
* [RayService][docs] Improve explanation for config file and in-place updates ([#1229](https://github.com/ray-project/kuberay/pull/1229), @zcin)
* [RayService][Doc] RayService troubleshooting handbook ([#1221](https://github.com/ray-project/kuberay/pull/1221), @kevin85421)
* [Doc] Improve RayService doc ([#1235](https://github.com/ray-project/kuberay/pull/1235), @kevin85421)
* [Doc] Improve FAQ page and RayService troubleshooting guide ([#1225](https://github.com/ray-project/kuberay/pull/1225), @kevin85421)
* [RayService] Add RayService alb ingress CR ([#1169](https://github.com/ray-project/kuberay/pull/1169), @sihanwang41)
* [RayService] Add support for multi-app config in yaml-string format ([#1156](https://github.com/ray-project/kuberay/pull/1156), @zcin)
* [rayservice] Add support for getting multi-app status ([#1136](https://github.com/ray-project/kuberay/pull/1136), @zcin)
* [Refactor] Remove Dashboard Agent service ([#1207](https://github.com/ray-project/kuberay/pull/1207), @kevin85421)
* [Bug] KubeRay operator fails to get serve deployment status due to 500 Internal Server Error ([#1173](https://github.com/ray-project/kuberay/pull/1173), @kevin85421)
* MobileNet example ([#1175](https://github.com/ray-project/kuberay/pull/1175), @kevin85421)
* [Bug] fix RayActorOptionSpec.items.spec.serveConfig.deployments.rayActorOptions.memory int32 data type ([#1220](https://github.com/ray-project/kuberay/pull/1220), @kevin85421)

### RayJob

* [RayJob] Submit job using K8s job instead of checking Status and using DashboardHTTPClient ([#1177](https://github.com/ray-project/kuberay/pull/1177), @architkulkarni)
* [Doc] [RayJob] Add documentation for submitterPodTemplate ([#1228](https://github.com/ray-project/kuberay/pull/1228), @architkulkarni)

### Autoscaler

* [release blocker][Feature] Only Autoscaler can make decisions to delete Pods ([#1253](https://github.com/ray-project/kuberay/pull/1253), @kevin85421)
* [release blocker][Autoscaler] Randomly delete Pods when scaling down the cluster ([#1251](https://github.com/ray-project/kuberay/pull/1251), @kevin85421)

### Helm

* [Helm][RBAC] Introduce the option crNamespacedRbacEnable to enable or disable the creation of Role/RoleBinding for RayCluster preparation ([#1162](https://github.com/ray-project/kuberay/pull/1162), @kevin85421)
* [Bug] Allow zero replica for workers for Helm ([#968](https://github.com/ray-project/kuberay/pull/968), @ducviet00)
* [Bug] KubeRay tries to create ClusterRoleBinding when singleNamespaceInstall and rbacEnable are set to true ([#1190](https://github.com/ray-project/kuberay/pull/1190), @kevin85421)

### KubeRay API Server

* Add support for openshift routes ([#1183](https://github.com/ray-project/kuberay/pull/1183), @blublinsky)
* Adding API server support for service account ([#1148](https://github.com/ray-project/kuberay/pull/1148), @blublinsky)

### Documentation

* [release v0.6.0] Update tags and versions ([#1270](https://github.com/ray-project/kuberay/pull/1270), @kevin85421)
* [release v0.6.0-rc.1] Update tags and versions ([#1264](https://github.com/ray-project/kuberay/pull/1264), @kevin85421)
* [release v0.6.0-rc.0] Update tags and versions ([#1237](https://github.com/ray-project/kuberay/pull/1237), @kevin85421)
* [Doc] Develop Ray Serve Python script on KubeRay ([#1250](https://github.com/ray-project/kuberay/pull/1250), @kevin85421)
* [Doc] Fix the order of comments in sample Job YAML file ([#1242](https://github.com/ray-project/kuberay/pull/1242), @architkulkarni)
* [Doc] Upload a screenshot for the Serve page in Ray dashboard ([#1236](https://github.com/ray-project/kuberay/pull/1236), @kevin85421)
* [Doc] GKE GPU cluster setup ([#1223](https://github.com/ray-project/kuberay/pull/1223), @kevin85421)
* [Doc][Website] Add complete document link ([#1224](https://github.com/ray-project/kuberay/pull/1224), @yuxiaoba)
* Add FAQ page ([#1150](https://github.com/ray-project/kuberay/pull/1150), @Yicheng-Lu-llll)
* [Doc] Add gofumpt lint instructions ([#1180](https://github.com/ray-project/kuberay/pull/1180), @architkulkarni)
* [Doc] Add `helm update` command to chart validation step in release process ([#1165](https://github.com/ray-project/kuberay/pull/1165), @architkulkarni)
* [Doc] Add git fetch --tags command to release instructions ([#1164](https://github.com/ray-project/kuberay/pull/1164), @architkulkarni)
* Add KubeRay related blogs ([#1147](https://github.com/ray-project/kuberay/pull/1147), @tedhtchang)
* [2.5.0 Release] Change version numbers 2.4.0 -> 2.5.0 ([#1151](https://github.com/ray-project/kuberay/pull/1151), @ArturNiederfahrenhorst)
* [Sample YAML] Bump ray version in pod security YAML to 2.4.0 ([#1160](https://github.com/ray-project/kuberay/pull/1160), @architkulkarni)
* Add instruction to skip unit tests in DEVELOPMENT.md ([#1171](https://github.com/ray-project/kuberay/pull/1171), @architkulkarni)
* Fix typo ([#1241](https://github.com/ray-project/kuberay/pull/1241), @mmourafiq)
* Fix typo ([#1232](https://github.com/ray-project/kuberay/pull/1232), @mmourafiq)

### CI

* [CI] Add `kind`-in-Docker test to Buildkite CI ([#1243](https://github.com/ray-project/kuberay/pull/1243), @architkulkarni)
* [CI] Remove unnecessary release.yaml workflow ([#1168](https://github.com/ray-project/kuberay/pull/1168), @architkulkarni)

### Others

* Pin operator version in single namespace installation(#1193) ([#1210](https://github.com/ray-project/kuberay/pull/1210), @wjzhou)
* RayCluster updates status frequently ([#1211](https://github.com/ray-project/kuberay/pull/1211), @kevin85421)
* Improve the observability of the init container ([#1149](https://github.com/ray-project/kuberay/pull/1149), @Yicheng-Lu-llll)
* [Ray Observability] Disk usage in Dashboard ([#1152](https://github.com/ray-project/kuberay/pull/1152), @kevin85421)


## v0.5.2 (2023-06-14)

### Highlights

The KubeRay 0.5.2 patch release includes the following improvements.
* Allow specifying the entire headService and serveService YAML spec. Previously, only certain special fields such as `labels` and `annotations` were exposed to the user.
  * Expose entire head pod Service to the user ([#1040](https://github.com/ray-project/kuberay/pull/1040), [@architkulkarni](https://github.com/architkulkarni))
  * Exposing Serve Service ([#1117](https://github.com/ray-project/kuberay/pull/1117), [@kodwanis](https://github.com/kodwanis))
* RayService stability improvements
  * RayService object’s Status is being updated due to frequent reconciliation ([#1065](https://github.com/ray-project/kuberay/pull/1065), [@kevin85421](https://github.com/kevin85421))
  * [RayService] Submit requests to the Dashboard after the head Pod is running and ready ([#1074](https://github.com/ray-project/kuberay/pull/1074), [@kevin85421](https://github.com/kevin85421))
  * Fix in HeadPod Service Generation logic which was causing frequent reconciliation ([#1056](https://github.com/ray-project/kuberay/pull/1056), [@msumitjain](https://github.com/msumitjain))
* Allow watching multiple namespaces
  * [Feature] Watch CR in multiple namespaces with namespaced RBAC resources ([#1106](https://github.com/ray-project/kuberay/pull/1106), [@kevin85421](https://github.com/kevin85421))
* Autoscaler stability improvements
   * [Bug] RayService restarts repeatedly with Autoscaler ([#1037](https://github.com/ray-project/kuberay/pull/1037), [@kevin85421](https://github.com/kevin85421))
  * [Bug] autoscaler not working properly in rayjob ([#1064](https://github.com/ray-project/kuberay/pull/1064), [@Yicheng-Lu-llll](https://github.com/Yicheng-Lu-llll))
  * [Bug][Autoscaler] Operator does not remove workers ([#1139](https://github.com/ray-project/kuberay/pull/1139), [@kevin85421](https://github.com/kevin85421))


### Contributors

We'd like to thank the following contributors for their contributions to this release:

@ByronHsu, @Yicheng-Lu-llll, @anishasthana, @architkulkarni, @blublinsky, @chrisxstyles, @dirtyValera, @ecurtin, @jasoonn, @jjyao, @kevin85421, @kodwanis, @msumitjain, @oginskis, @psschwei, @scarlet25151, @sihanwang41, @tedhtchang, @varungup90, @xubo245

### Features

* Add a flag to enable/disable worker init container injection ([#1069](https://github.com/ray-project/kuberay/pull/1069), @ByronHsu)
* Add a warning to discourage users from launching a KubeRay-incompatible autoscaler. ([#1102](https://github.com/ray-project/kuberay/pull/1102), @kevin85421)
* Add consistency check for deepcopy generated files ([#1127](https://github.com/ray-project/kuberay/pull/1127), @varungup90)
* Add kubernetes dependency in python client library ([#998](https://github.com/ray-project/kuberay/pull/998), @jasoonn)
* Add support for pvcs to apiserver ([#1118](https://github.com/ray-project/kuberay/pull/1118), @psschwei)
* Add support for tolerations, env, annotations and labels ([#1070](https://github.com/ray-project/kuberay/pull/1070), @blublinsky)
* Align Init Container's ImagePullPolicy with Ray Container's ImagePullPolicy ([#1080](https://github.com/ray-project/kuberay/pull/1080), @Yicheng-Lu-llll)
* Connect Ray client with TLS using Nginx Ingress on Kind cluster (#729) ([#1051](https://github.com/ray-project/kuberay/pull/1051), @tedhtchang)
* Expose entire head pod Service to the user ([#1040](https://github.com/ray-project/kuberay/pull/1040), @architkulkarni)
* Exposing Serve Service ([#1117](https://github.com/ray-project/kuberay/pull/1117), @kodwanis)
* [Test] Add e2e test for sample RayJob yaml on kind ([#935](https://github.com/ray-project/kuberay/pull/935), @architkulkarni)
* Parametrize ray-operator makefile ([#1121](https://github.com/ray-project/kuberay/pull/1121), @anishasthana)
* RayService object's Status is being updated due to frequent reconciliation ([#1065](https://github.com/ray-project/kuberay/pull/1065), @kevin85421)
* [Feature] Support suspend in RayJob ([#926](https://github.com/ray-project/kuberay/pull/926), @oginskis)
* [Feature] Watch CR in multiple namespaces with namespaced RBAC resources ([#1106](https://github.com/ray-project/kuberay/pull/1106), @kevin85421)
* [RayService] Submit requests to the Dashboard after the head Pod is running and ready ([#1074](https://github.com/ray-project/kuberay/pull/1074), @kevin85421)
* feat: Rename instances of rayiov1alpha1 to rayv1alpha1 ([#1112](https://github.com/ray-project/kuberay/pull/1112), @anishasthana)
* ray-operator: Reuse contexts across ray operator reconcilers ([#1126](https://github.com/ray-project/kuberay/pull/1126), @anishasthana)

### Fixes

* Fix CI ([#1145](https://github.com/ray-project/kuberay/pull/1145), @kevin85421)
* Fix config frequent update ([#1014](https://github.com/ray-project/kuberay/pull/1014), @sihanwang41)
* Fix for Sample YAML Config Test - 2.4.0 Failure due to 'suspend' Field  ([#1096](https://github.com/ray-project/kuberay/pull/1096), @Yicheng-Lu-llll)
* Fix in HeadPod Service Generation logic which was causing frequent reconciliation ([#1056](https://github.com/ray-project/kuberay/pull/1056), @msumitjain)
* [Bug] Autoscaler doesn't support TLS ([#1119](https://github.com/ray-project/kuberay/pull/1119), @chrisxstyles)
* [Bug] Enable ResourceQuota by adding Resources for the health-check init container ([#1043](https://github.com/ray-project/kuberay/pull/1043), @kevin85421)
* [Bug] Fix null map handling in `BuildServiceForHeadPod` function ([#1095](https://github.com/ray-project/kuberay/pull/1095), @architkulkarni)
* [Bug] RayService restarts repeatedly with Autoscaler  ([#1037](https://github.com/ray-project/kuberay/pull/1037), @kevin85421)
* [Bug] Service (Serve) changing port from 8000 to 9000 doesn't work ([#1081](https://github.com/ray-project/kuberay/pull/1081), @kevin85421)
* [Bug] autoscaler not working properly in rayjob  ([#1064](https://github.com/ray-project/kuberay/pull/1064), @Yicheng-Lu-llll)
* [Bug] compatibility test for the nightly Ray image fails ([#1055](https://github.com/ray-project/kuberay/pull/1055), @kevin85421)
* [Bug] rayStartParams is required at this moment. ([#1031](https://github.com/ray-project/kuberay/pull/1031), @kevin85421)
* [Bug][Autoscaler] Operator does not remove workers ([#1139](https://github.com/ray-project/kuberay/pull/1139), @kevin85421)
* [Bug][Doc] fix the link error of operator document ([#1046](https://github.com/ray-project/kuberay/pull/1046), @xubo245)
* [Bug][GCS FT] Worker pods crash unexpectedly when gcs_server on head pod is killed ([#1036](https://github.com/ray-project/kuberay/pull/1036), @kevin85421)
* [Bug][breaking change] Unauthorized 401 error on fetching Ray Custom Resources from K8s API server ([#1128](https://github.com/ray-project/kuberay/pull/1128), @kevin85421)
* [Bug][k8s compatibility] k8s v1.20.7 ClusterIP svc do not updated under RayService  ([#1110](https://github.com/ray-project/kuberay/pull/1110), @kevin85421)
* [Helm][ray-cluster] Fix parsing envFrom field in additionalWorkerGroups ([#1039](https://github.com/ray-project/kuberay/pull/1039), @dirtyValera)

### Documentation
* [Doc] Copyedit dev guide ([#1012](https://github.com/ray-project/kuberay/pull/1012), @architkulkarni)
* [Doc] Update nav to include missing files and reorganize nav ([#1011](https://github.com/ray-project/kuberay/pull/1011), @architkulkarni)
* [Doc] Update version from 0.4.0 to 0.5.0 on remaining kuberay docs files ([#1018](https://github.com/ray-project/kuberay/pull/1018), @architkulkarni)
* [Doc][Website] Update KubeRay introduction and fix layout issues ([#1042](https://github.com/ray-project/kuberay/pull/1042), @kevin85421)
* [Docs][Website] One word typo fix in docs and README ([#1068](https://github.com/ray-project/kuberay/pull/1068), @ecurtin)
* Add a document to outline the default settings for `rayStartParams` in Kuberay ([#1057](https://github.com/ray-project/kuberay/pull/1057), @Yicheng-Lu-llll)
* Example Pod to connect Ray client to remote a Ray cluster with TLS enabled ([#994](https://github.com/ray-project/kuberay/pull/994), @tedhtchang)
* [Post release v0.5.0] Update CHANGELOG.md ([#1026](https://github.com/ray-project/kuberay/pull/1026), @kevin85421)
* [Post release v0.5.0] Update release doc ([#1028](https://github.com/ray-project/kuberay/pull/1028), @kevin85421)
* [Post Ray 2.4 Release] Update Ray versions to Ray 2.4.0 ([#1049](https://github.com/ray-project/kuberay/pull/1049), @jjyao)
* [Post release v0.5.0] Remove block from rayStartParams ([#1015](https://github.com/ray-project/kuberay/pull/1015), @kevin85421)
* [Post release v0.5.0] Remove block from rayStartParams for python client and KubeRay operator tests  ([#1050](https://github.com/ray-project/kuberay/pull/1050), @Yicheng-Lu-llll)
* [Post release v0.5.0] Remove serviceType ([#1013](https://github.com/ray-project/kuberay/pull/1013), @kevin85421)
* [Post v0.5.0] Remove init containers from YAML files ([#1010](https://github.com/ray-project/kuberay/pull/1010), @kevin85421)
* [Sample YAML] Bump ray version in pod security YAML to 2.4.0 (#1160) ([#1161](https://github.com/ray-project/kuberay/pull/1161), @architkulkarni)
* Kuberay 0.5.0 docs validation update docs for GCS FT ([#1004](https://github.com/ray-project/kuberay/pull/1004), @scarlet25151)
* Release v0.5.0 doc validation ([#997](https://github.com/ray-project/kuberay/pull/997), @kevin85421)
* Release v0.5.0 doc validation part 2 ([#999](https://github.com/ray-project/kuberay/pull/999), @architkulkarni)
* Release v0.5.0 python client library validation ([#1006](https://github.com/ray-project/kuberay/pull/1006), @jasoonn)
* [release v0.5.2] Update tags and versions to 0.5.2 ([#1159](https://github.com/ray-project/kuberay/pull/1159), @architkulkarni)

## v0.5.0 (2023-04-11)

### Highlights

The KubeRay 0.5.0 release includes the following improvements.

* Interact with KubeRay via a [Python client](https://github.com/ray-project/kuberay/tree/master/clients/python-client)
* Integrate KubeRay with Kubeflow to provide an interactive development environment ([link](https://github.com/ray-project/kuberay/blob/master/docs/guidance/kubeflow-integration.md)).
* Integrate KubeRay with Ray [TLS authentication](https://github.com/ray-project/kuberay/blob/master/docs/guidance/tls.md)
* Improve the user experience for KubeRay on AWS EKS ([link](https://github.com/ray-project/kuberay/blob/master/docs/guidance/aws-eks-iam.md))
* Fix some Kubernetes networking issues
* Fix some stability bugs in RayJob and RayService

### Contributors

The following individuals contributed to KubeRay 0.5.0. This list is alphabetical and incomplete.

@akanso @alex-treebeard @architkulkarni @cadedaniel @cskornel-doordash @davidxia @Dmitrigekhtman @ducviet00 @gvspraveen @harryge00 @jasoonn @Jeffwan @kevin85421 @psschwei @scarlet25151 @sihanwang41 @wilsonwang371 @Yicheng-lu-llll

### Python client (alpha)(New!)
* Alkanso/python client ([#901](https://github.com/ray-project/kuberay/pull/901), @akanso)
* Reorganize python client library ([#984](https://github.com/ray-project/kuberay/pull/984), @jasoonn)

### Kubeflow (New!)
* [Feature][Doc] Kubeflow integration ([#937](https://github.com/ray-project/kuberay/pull/937), @kevin85421)
* [Feature] Ray restricted podsecuritystandards for enterprise security and Kubeflow integration ([#750](https://github.com/ray-project/kuberay/pull/750), @kevin85421)

### TLS authentication (New!)
* [Feature] TLS authentication ([#989](https://github.com/ray-project/kuberay/pull/989), @kevin85421)

### AWS EKS (New!)
* [Feature][Doc] Access S3 bucket from Pods in EKS ([#958](https://github.com/ray-project/kuberay/pull/958), @kevin85421)

### Kubernetes networking (New!)
* Read cluster domain from resolv.conf or env ([#951](https://github.com/ray-project/kuberay/pull/951), @harryge00)
* [Feature] Replace service name with Fully Qualified Domain Name  ([#938](https://github.com/ray-project/kuberay/pull/938), @kevin85421)
* [Feature] Add default init container in workers to wait for GCS to be ready ([#973](https://github.com/ray-project/kuberay/pull/973), @kevin85421)

### Observability
* Fix issue with head pod not monitered by Prometheus under certain condition ([#963](https://github.com/ray-project/kuberay/pull/963), @Yicheng-Lu-llll)
* [Feature] Improve and fix Prometheus & Grafana integrations ([#895](https://github.com/ray-project/kuberay/pull/895), @kevin85421)
* Add example and tutorial to explain how to create custom metrics for Prometheus ([#914](https://github.com/ray-project/kuberay/pull/914), @Yicheng-Lu-llll)
* feat: enrich `kubectl get` output ([#878](https://github.com/ray-project/kuberay/pull/878), @davidxia)

### RayCluster
* Fix issue with operator OOM restart ([#946](https://github.com/ray-project/kuberay/pull/946), @wilsonwang371)
* [Feature][Hotfix] Add observedGeneration to the status of CRDs ([#979](https://github.com/ray-project/kuberay/pull/979), @kevin85421)
* Customize the Prometheus export port ([#954](https://github.com/ray-project/kuberay/pull/954), @Yicheng-Lu-llll)
* [Feature] The default ImagePullPolicy should be IfNotPresent ([#947](https://github.com/ray-project/kuberay/pull/947), @kevin85421)
* Inject the --block option to ray start command automatically ([#932](https://github.com/ray-project/kuberay/pull/932), @Yicheng-Lu-llll)
* Inject cluster name as an environment variable into head and worker pods ([#934](https://github.com/ray-project/kuberay/pull/934), @Yicheng-Lu-llll)
* Ensure container ports without names are also included in the head node service ([#891](https://github.com/ray-project/kuberay/pull/891), @Yicheng-Lu-llll)
* fix: `.status.availableWorkerReplicas` ([#887](https://github.com/ray-project/kuberay/pull/887), @davidxia)
* fix: only filter RayCluster events for reconciliation ([#882](https://github.com/ray-project/kuberay/pull/882), @davidxia)
* refactor: remove redundant import in `raycluster_controller.go` ([#884](https://github.com/ray-project/kuberay/pull/884), @davidxia)
* refactor: use equivalent, shorter `Builder.Owns()` method ([#881](https://github.com/ray-project/kuberay/pull/881), @davidxia)
* [RayCluster controller] [Bug] Unconditionally reconcile RayCluster every 60s instead of only upon change ([#850](https://github.com/ray-project/kuberay/pull/850), @architkulkarni)
* [Feature] Make head serviceType optional ([#851](https://github.com/ray-project/kuberay/pull/851), @kevin85421)
* [RayCluster controller] Add headServiceAnnotations field to RayCluster CR ([#841](https://github.com/ray-project/kuberay/pull/841), @cskornel-doordash)

### RayJob (alpha)
* [Hotfix][release blocker][RayJob] HTTP client from submitting jobs before dashboard initialization completes ([#1000](https://github.com/ray-project/kuberay/pull/1000), @kevin85421)
* [RayJob] Propagate error traceback string when GetJobInfo doesn't return valid JSON ([#943](https://github.com/ray-project/kuberay/pull/943), @architkulkarni)
* [RayJob][Doc] Fix RayJob sample config. ([#807](https://github.com/ray-project/kuberay/pull/807), @DmitriGekhtman)

### RayService (alpha)
* [RayService] Skip update events without change ([#811](https://github.com/ray-project/kuberay/pull/811), @sihanwang41)

### Helm
* Add rayVersion in the RayCluster chart ([#975](https://github.com/ray-project/kuberay/pull/975), @Yicheng-Lu-llll)
* [Feature] Support environment variables for KubeRay operator chart ([#978](https://github.com/ray-project/kuberay/pull/978), @kevin85421)
* [Feature] Add service account section in helm chart ([#969](https://github.com/ray-project/kuberay/pull/969), @ducviet00)
* Update apiserver chart location in readme ([#896](https://github.com/ray-project/kuberay/pull/896), @psschwei)
* add sidecar container option ([#920](https://github.com/ray-project/kuberay/pull/920), @akihikokuroda)
* match selector of service to pod labels ([#918](https://github.com/ray-project/kuberay/pull/918), @akihikokuroda)
* [Feature] Nodeselector/Affinity/Tolerations value to kuberay-apiserver chart ([#879](https://github.com/ray-project/kuberay/pull/879), @alex-treebeard)
* [Feature] Enable namespaced installs via helm chart ([#860](https://github.com/ray-project/kuberay/pull/860), @alex-treebeard)
* Remove unused fields from KubeRay operator and RayCluster charts ([#839](https://github.com/ray-project/kuberay/pull/839), @kevin85421)
* [Bug] Remove an unused field (ingress.enabled) from KubeRay operator chart ([#812](https://github.com/ray-project/kuberay/pull/812), @kevin85421)
* [helm] Add memory limits and resource documentation. ([#789](https://github.com/ray-project/kuberay/pull/789), @DmitriGekhtman)

### CI
* [Feature] Add python client test to action ([#993](https://github.com/ray-project/kuberay/pull/993), @jasoonn)
* [CI][Buildkite] Fix the PATH issue ([#952](https://github.com/ray-project/kuberay/pull/952), @kevin85421)
* [CI][Buildkite] An example test for Buildkite ([#919](https://github.com/ray-project/kuberay/pull/919), @kevin85421)
* refactor: Fix flaky tests by using RetryOnConflict ([#904](https://github.com/ray-project/kuberay/pull/904), @Yicheng-Lu-llll)
* Use k8sClient from client.New in controller test ([#898](https://github.com/ray-project/kuberay/pull/898), @Yicheng-Lu-llll)
* [Bug] Fix flaky test: should be able to update all Pods to Running ([#893](https://github.com/ray-project/kuberay/pull/893), @kevin85421)
* Enable test framework to install operator with custom config and put operator in a namespace with enforced PSS in security testing ([#876](https://github.com/ray-project/kuberay/pull/876), @Yicheng-Lu-llll)
* Ensure all temp files are deleted after the compatibility test ([#886](https://github.com/ray-project/kuberay/pull/886), @Yicheng-Lu-llll)
* Adding a test for the document for the Pod security standard ([#866](https://github.com/ray-project/kuberay/pull/866), @Yicheng-Lu-llll)
* [Feature] Run config tests with the latest release of KubeRay operator ([#858](https://github.com/ray-project/kuberay/pull/858), @kevin85421)
* [Feature] Define a general-purpose cleanup method for CREvent ([#849](https://github.com/ray-project/kuberay/pull/849), @kevin85421)
* [Feature] Remove Docker container and NodePort from compatibility test ([#844](https://github.com/ray-project/kuberay/pull/844), @kevin85421)
* Remove Docker from BasicRayTestCase ([#840](https://github.com/ray-project/kuberay/pull/840), @kevin85421)
* [Feature] Move some functions from prototype test framework to a new utils file ([#837](https://github.com/ray-project/kuberay/pull/837), @kevin85421)
* [CI] Add workflow to manually trigger release image push ([#801](https://github.com/ray-project/kuberay/pull/801), @DmitriGekhtman)
* [CI] Pin go version in CRD consistency check ([#794](https://github.com/ray-project/kuberay/pull/794), @DmitriGekhtman)
* [Feature] Improve the observability of integration tests ([#775](https://github.com/ray-project/kuberay/pull/775), @jasoonn)

### Sample YAML files
* Improve ray-cluster.external-redis.yaml ([#986](https://github.com/ray-project/kuberay/pull/986), @Yicheng-Lu-llll)
* remove ray-cluster.getting-started.yaml ([#987](https://github.com/ray-project/kuberay/pull/987), @Yicheng-Lu-llll)
* [Feature] Read Redis password from Kubernetes Secret ([#950](https://github.com/ray-project/kuberay/pull/950), @kevin85421)
* [Ray 2.3.0] Update --redis-password for RayCluster ([#929](https://github.com/ray-project/kuberay/pull/929), @kevin85421)
* [Bug] KubeRay does not work on M1 macs. ([#869](https://github.com/ray-project/kuberay/pull/869), @kevin85421)
* [Post Ray 2.3 Release] Update Ray versions to Ray 2.3.0 ([#925](https://github.com/ray-project/kuberay/pull/925), @cadedaniel)
* [Post Ray 2.2.0 Release] Update Ray versions to Ray 2.2.0 ([#822](https://github.com/ray-project/kuberay/pull/822), @DmitriGekhtman)

### Documentation
* Update contribution doc to show users how to reach out via slack ([#936](https://github.com/ray-project/kuberay/pull/936), @gvspraveen)
* [Feature][Docs] Explain how to specify container command for head pod ([#912](https://github.com/ray-project/kuberay/pull/912), @kevin85421)
* [post-0.4.0 KubeRay release] update proto version to 0.4.0 ([#830](https://github.com/ray-project/kuberay/pull/830), @scarlet25151)
* [0.4.0 release] Update changelog for KubeRay 0.4.0 ([#836](https://github.com/ray-project/kuberay/pull/836), @DmitriGekhtman)
* [Docs] Revise release note docs ([#835](https://github.com/ray-project/kuberay/pull/835), @DmitriGekhtman)
* [release] Add release command and guidance for KubeRay cli ([#834](https://github.com/ray-project/kuberay/pull/834), @Jeffwan)
* [Release] Add tools and docs for changelog generator ([#833](https://github.com/ray-project/kuberay/pull/833), @Jeffwan)
* [Bug] error: git cmd when following docs ([#831](https://github.com/ray-project/kuberay/pull/831), @kevin85421)
* [post-0.4.0 KubeRay release] Update KubeRay versions  ([#821](https://github.com/ray-project/kuberay/pull/821), @DmitriGekhtman)
* [Feature][Doc] End-to-end KubeRay operator development process on Kind ([#826](https://github.com/ray-project/kuberay/pull/826), @kevin85421)
* [Release][Docs] Update release instructions ([#819](https://github.com/ray-project/kuberay/pull/819), @DmitriGekhtman)
* [docs] Tweaks to main README, add basic API Server README. ([#809](https://github.com/ray-project/kuberay/pull/809), @DmitriGekhtman)
* update docs for release v0.4.0 ([#778](https://github.com/ray-project/kuberay/pull/778), @scarlet25151)
* [docs] Update KubeRay operator README.  ([#808](https://github.com/ray-project/kuberay/pull/808), @DmitriGekhtman)
* [Release] Update docs for release v0.4.0 ([#779](https://github.com/ray-project/kuberay/pull/779), @kevin85421)

## v0.4.0 (2022-12-12)

### Highlights

The KubeRay 0.4.0 release includes the following improvements.

* Integrations for the [MCAD](https://ray-project.github.io/kuberay/guidance/kuberay-with-MCAD/)
and [Volcano](https://ray-project.github.io/kuberay/guidance/volcano-integration/) batch scheduling systems.
* Stable Helm support for the [KubeRay Operator](https://github.com/ray-project/kuberay/blob/master/helm-chart/kuberay-operator/README.md), [KubeRay API Server](https://github.com/ray-project/kuberay/tree/master/helm-chart/kuberay-apiserver#readme), and [Ray clusters](https://github.com/ray-project/kuberay/blob/master/helm-chart/ray-cluster/README.md). These charts are now hosted at a [Helm repo](https://github.com/ray-project/kuberay-helm).
* Critical stability improvements to the [Ray Autoscaler integration](https://ray-project.github.io/kuberay/guidance/autoscaler/). (To benefit from these improvements, use KubeRay >=0.4.0 and Ray >=2.2.0.)
* Numerous improvements to CI, tests, and developer workflows; a new [configuration test framework](https://github.com/ray-project/kuberay/pull/605).
* Numerous improvements to documentation.
* Bug fixes for alpha features, such as [RayJobs](https://ray-project.github.io/kuberay/guidance/rayjob/) and [RayServices](https://ray-project.github.io/kuberay/guidance/rayservice/).
* Various improvements and bug fixes for the core RayCluster controller.

### Contributors

The following individuals contributed to KubeRay 0.4.0. This list is alphabetical and incomplete.

@AlessandroPomponio @architkulkarni @Basasuya @DmitriGekhtman @IceKhan13 @asm582 @davidxia @dhaval0108 @haoxins @iycheng @jasoonn @Jeffwan @jianyuan @kaushik143 @kevin85421 @lizzzcai @orcahmlee @pcmoritz @peterghaddad @rafvasq @scarlet25151 @shrekris-anyscale @sigmundv @sihanwang41 @simon-mo @tbabej @tgaddair @ulfox @wilsonwang371 @wuisawesome

### New features and integrations

* [Feature] Support Volcano for batch scheduling ([#755](https://github.com/ray-project/kuberay/pull/755), @tgaddair)
* kuberay int with MCAD ([#598](https://github.com/ray-project/kuberay/pull/598), @asm582)

### Helm

These changes pertain to KubeRay's Helm charts.

* [Bug] Remove an unused field (ingress.enabled) from KubeRay operator chart ([#812](https://github.com/ray-project/kuberay/pull/812), @kevin85421)
* [helm] Add memory limits and resource documentation. ([#789](https://github.com/ray-project/kuberay/pull/789), @DmitriGekhtman)
* [Helm] Expose security context in helm chart. ([#773](https://github.com/ray-project/kuberay/pull/773), @DmitriGekhtman)
* [Helm] Clean up RayCluster Helm chart ahead of KubeRay 0.4.0 release ([#751](https://github.com/ray-project/kuberay/pull/751), @DmitriGekhtman)
* [Feature] Expose initContainer image in RayCluster chart ([#674](https://github.com/ray-project/kuberay/pull/674), @kevin85421)
* [Feature][Helm] Expose the autoscalerOptions ([#666](https://github.com/ray-project/kuberay/pull/666), @orcahmlee)
* [Feature][Helm] Align the key of minReplicas and maxReplicas ([#663](https://github.com/ray-project/kuberay/pull/663), @orcahmlee)
* Helm: add service type configuration to head group for ray-cluster ([#614](https://github.com/ray-project/kuberay/pull/614), @IceKhan13)
* Allow annotations in ray cluster helm chart ([#574](https://github.com/ray-project/kuberay/pull/574), @sigmundv)
* [Feature][Helm] Enable sidecar configuration in Helm chart ([#604](https://github.com/ray-project/kuberay/pull/604), @kevin85421)
* [bugfix][apiserver helm]: Adding missing rbacenable value ([#594](https://github.com/ray-project/kuberay/pull/594), @dhaval0108)
* [Bug] Modification of nameOverride will cause label selector mismatch for head node ([#572](https://github.com/ray-project/kuberay/pull/572), @kevin85421)
* [Helm][minor] Make "disabled" flag for worker groups optional ([#548](https://github.com/ray-project/kuberay/pull/548), @kevin85421)
* helm: Uncomment the disabled key for the default workergroup ([#543](https://github.com/ray-project/kuberay/pull/543), @tbabej)
* Fix Helm chart default configuration ([#530](https://github.com/ray-project/kuberay/pull/530), @kevin85421)
* helm-chart/ray-cluster: Allow setting pod lifecycle ([#494](https://github.com/ray-project/kuberay/pull/494), @ulfox)

### CI

The changes in this section pertain to KubeRay CI, testing, and developer workflows.

* [Feature] Improve the observability of integration tests ([#775](https://github.com/ray-project/kuberay/pull/775), @jasoonn)
* [CI] Pin go version in CRD consistency check ([#794](https://github.com/ray-project/kuberay/pull/794), @DmitriGekhtman)
* [Feature] Test sample RayService YAML to catch invalid or out of date one ([#731](https://github.com/ray-project/kuberay/pull/731), @jasoonn)
* Replace kubectl wait command with RayClusterAddCREvent ([#705](https://github.com/ray-project/kuberay/pull/705), @kevin85421)
* [Feature] Test sample RayCluster YAMLs to catch invalid or out of date ones ([#678](https://github.com/ray-project/kuberay/pull/678), @kevin85421)
* [Bug] Misuse of Docker API and misunderstanding of Ray HA cause test_ray_serve flaky ([#650](https://github.com/ray-project/kuberay/pull/650), @jasoonn)
* Configuration Test Framework Prototype ([#605](https://github.com/ray-project/kuberay/pull/605), @kevin85421)
* Update tests for better Mac M1 compatibility ([#654](https://github.com/ray-project/kuberay/pull/654), @shrekris-anyscale)
* [Bug] Update wait function in test_detached_actor ([#635](https://github.com/ray-project/kuberay/pull/635), @kevin85421)
* [Bug] Misuse of Docker API and misunderstanding of Ray HA cause test_detached_actor flaky ([#619](https://github.com/ray-project/kuberay/pull/619), @kevin85421)
* [Feature] Docker support for chart-testing ([#623](https://github.com/ray-project/kuberay/pull/623), @jasoonn)
* [Feature] Optimize the wait functions in E2E tests ([#609](https://github.com/ray-project/kuberay/pull/609), @kevin85421)
* [Feature] Running end-to-end tests on local machine ([#589](https://github.com/ray-project/kuberay/pull/589), @kevin85421)
* [CI]use fixed version of gofumpt ([#596](https://github.com/ray-project/kuberay/pull/596), @wilsonwang371)
* update test files before separating them ([#591](https://github.com/ray-project/kuberay/pull/591), @wilsonwang371)
* Add reminders to avoid RBAC synchronization bug ([#576](https://github.com/ray-project/kuberay/pull/576), @kevin85421)
* [Feature] Consistency check for RBAC ([#577](https://github.com/ray-project/kuberay/pull/577), @kevin85421)
* [Feature] Sync for manifests and helm chart ([#564](https://github.com/ray-project/kuberay/pull/564), @kevin85421)
* [Feature] Add a chart-test script to enable chart lint error reproduction on laptop ([#563](https://github.com/ray-project/kuberay/pull/563), @kevin85421)
* [Feature] Add helm lint check in Github Actions ([#554](https://github.com/ray-project/kuberay/pull/554), @kevin85421)
* [Feature] Add consistency check for types.go, CRDs, and generated API in GitHub Actions ([#546](https://github.com/ray-project/kuberay/pull/546), @kevin85421)
* support ray 2.0.0 in compatibility test ([#508](https://github.com/ray-project/kuberay/pull/508), @wilsonwang371)

### KubeRay Operator deployment

The changes in this section pertain to deployment of the KubeRay Operator.

* Fix finalizer typo and re-create manifests ([#631](https://github.com/ray-project/kuberay/pull/631), @AlessandroPomponio)
* Change Kuberay operator Deployment strategy type to Recreate ([#566](https://github.com/ray-project/kuberay/pull/566), @haoxins)
* [Bug][Doc] Increase default operator resource requirements, improve docs ([#727](https://github.com/ray-project/kuberay/pull/727), @kevin85421)
* [Feature] Sync logs to local file ([#632](https://github.com/ray-project/kuberay/pull/632), @Basasuya)
* [Bug] label rayNodeType is useless ([#698](https://github.com/ray-project/kuberay/pull/698), @kevin85421)
* Revise sample configs, increase memory requests, update Ray versions ([#761](https://github.com/ray-project/kuberay/pull/761), @DmitriGekhtman)

### RayCluster controller

The changes in this section pertain to the RayCluster controller sub-component of the KubeRay Operator.

* [autoscaler] Expose autoscaler container security context. ([#752](https://github.com/ray-project/kuberay/pull/752), @DmitriGekhtman)
* refactor: log more descriptive info from initContainer ([#526](https://github.com/ray-project/kuberay/pull/526), @davidxia)
* [Bug] Fail to create ingress due to the deprecation of the ingress.class annotation ([#646](https://github.com/ray-project/kuberay/pull/646), @kevin85421)
* [kuberay] Fix inconsistent RBAC truncation for autoscaling clusters. ([#689](https://github.com/ray-project/kuberay/pull/689), @DmitriGekhtman)
* [raycluster controller] Always honor maxReplicas ([#662](https://github.com/ray-project/kuberay/pull/662), @DmitriGekhtman)
* [Autoscaler] Pass pod name to autoscaler, add pod patch permission ([#740](https://github.com/ray-project/kuberay/pull/740), @DmitriGekhtman)
* [Bug] Shallow copy causes different worker configurations ([#714](https://github.com/ray-project/kuberay/pull/714), @kevin85421)
* Fix duplicated volume issue ([#690](https://github.com/ray-project/kuberay/pull/690), @wilsonwang371)
* [fix][raycluster controller] No error if head ip cannot be determined. ([#701](https://github.com/ray-project/kuberay/pull/701), @DmitriGekhtman)
* [Feature] Set default appProtocol for Ray head service to tcp ([#668](https://github.com/ray-project/kuberay/pull/668), @kevin85421)
* [Telemetry] Inject env identifying KubeRay. ([#562](https://github.com/ray-project/kuberay/pull/562), @DmitriGekhtman)
* fix: correctly set GPUs in rayStartParams ([#497](https://github.com/ray-project/kuberay/pull/497), @davidxia)
* [operator] enable bashrc before container start ([#427](https://github.com/ray-project/kuberay/pull/427), @Basasuya)
* [Bug] Pod reconciliation fails if worker pod name is supplied ([#587](https://github.com/ray-project/kuberay/pull/587), @kevin85421)

### Ray Jobs (alpha)

The changes pertain to the RayJob controller sub-component of the KubeRay Operator.

* [Feature] [RayJobs] Use finalizers to implement stopping a job upon cluster deletion ([#735](https://github.com/ray-project/kuberay/pull/735), @kevin85421)
* [ray job] support stop job after job cr is deleted in cluster selector mode ([#629](https://github.com/ray-project/kuberay/pull/629), @Basasuya)
* [RayJob] Fix example misconfiguration. ([#602](https://github.com/ray-project/kuberay/pull/602), @DmitriGekhtman)
* [operator] support clusterselector in job crd ([#470](https://github.com/ray-project/kuberay/pull/470), @Basasuya)

### Ray Services (alpha)

The changes pertain to the RayService controller sub-component of the KubeRay Operator.

* [RayService] Skip update events without change ([#811](https://github.com/ray-project/kuberay/pull/811), @sihanwang41)
* [RayService] Track whether Serve app is ready before switching clusters ([#730](https://github.com/ray-project/kuberay/pull/730), @shrekris-anyscale)
* [RayService] Compare cached hashed config before triggering update ([#655](https://github.com/ray-project/kuberay/pull/655), @shrekris-anyscale)
* Disable async serve handler in Ray Service cluster. ([#447](https://github.com/ray-project/kuberay/pull/447), @iycheng)
* [RayService] Revert "Disable async serve handler in Ray Service cluster (#447)" ([#606](https://github.com/ray-project/kuberay/pull/606), @shrekris-anyscale)
* add support for rayserve in apiserver ([#456](https://github.com/ray-project/kuberay/pull/456), @scarlet25151)
* Fix initial health check not obeying deploymentUnhealthySecondThreshold ([#540](https://github.com/ray-project/kuberay/pull/540), @jianyuan)

### KubeRay API Server

* [Bug][apiserver] fix apiserver create rayservice missing serve port ([#734](https://github.com/ray-project/kuberay/pull/734), @scarlet25151)
* Support updating RayServices using the KubeRay API Server ([#633](https://github.com/ray-project/kuberay/pull/633), @scarlet25151)
* [api server] enable job spec server ([#416](https://github.com/ray-project/kuberay/pull/416), @Basasuya)

### Security

* [Bug] client_golang used by KubeRay has a vulnerability ([#728](https://github.com/ray-project/kuberay/pull/728), @kevin85421)

### Observability

* feat: update RayCluster `.status.reason` field with pod creation error ([#639](https://github.com/ray-project/kuberay/pull/639), @davidxia)
* feat: enrich RayCluster status with head IPs ([#468](https://github.com/ray-project/kuberay/pull/468), @davidxia)
* config/prometheus: add metrics exporter for workers ([#469](https://github.com/ray-project/kuberay/pull/469), @ulfox)

### Documentation

* [docs] Updated Volcano integration documentation ([#776](https://github.com/ray-project/kuberay/pull/776), @tgaddair)
* [0.4.0 Release] Minor doc improvements ([#780](https://github.com/ray-project/kuberay/pull/780), @DmitriGekhtman)
* Update gcs-ft.md ([#777](https://github.com/ray-project/kuberay/pull/777), @wilsonwang371)
* [Feature] Refactor test framework & test kuberay-operator chart with configuration framework ([#759](https://github.com/ray-project/kuberay/pull/759), @kevin85421)
* fix docs: typo in README.md ([#760](https://github.com/ray-project/kuberay/pull/760), @davidxia)
* [APIServer][Docs] Identify API server as community-managed and optional ([#753](https://github.com/ray-project/kuberay/pull/753), @DmitriGekhtman)
* Add documentations for the release process of Helm charts ([#723](https://github.com/ray-project/kuberay/pull/723), @kevin85421)
* [docs] Fix markdown in ray services ([#712](https://github.com/ray-project/kuberay/pull/712), @lizzzcai)
* Cross-reference docs. ([#703](https://github.com/ray-project/kuberay/pull/703), @DmitriGekhtman)
* Adding example of manually setting up NGINX Ingress ([#699](https://github.com/ray-project/kuberay/pull/699), @jasoonn)
* [docs] State version requirement for kubectl ([#702](https://github.com/ray-project/kuberay/pull/702), @DmitriGekhtman)
* Remove ray-cluster.without-block.yaml ([#675](https://github.com/ray-project/kuberay/pull/675), @kevin85421)
* [doc] Add instructions about how to use SSL/TLS for redis connection. ([#652](https://github.com/ray-project/kuberay/pull/652), @iycheng)
* [Feature][Docs] AWS Application Load Balancer (ALB) support ([#658](https://github.com/ray-project/kuberay/pull/658), @kevin85421)
* [Feature][Doc] Explain that RBAC should be synchronized manually ([#641](https://github.com/ray-project/kuberay/pull/641), @kevin85421)
* [doc] Reformat README.md ([#599](https://github.com/ray-project/kuberay/pull/599), @rafvasq)
* [doc] Copy-Edit RayJob ([#608](https://github.com/ray-project/kuberay/pull/608), @rafvasq)
* [doc] VS Code IDE setup ([#613](https://github.com/ray-project/kuberay/pull/613), @kevin85421)
* [doc] Copy-Edit RayService ([#607](https://github.com/ray-project/kuberay/pull/607), @rafvasq)
* fix mkdocs URL ([#600](https://github.com/ray-project/kuberay/pull/600), @asm582)
* [doc] Add a tip on docker images ([#586](https://github.com/ray-project/kuberay/pull/586), @DmitriGekhtman)
* Update ray-operator documentation and image version in ray-cluster.heterogeneous.yaml ([#585](https://github.com/ray-project/kuberay/pull/585), @jasoonn)
* [Doc] Cannot build kuberay with Go 1.16 ([#575](https://github.com/ray-project/kuberay/pull/575), @kevin85421)
* docs: Add instructions for working with Argo CD ([#535](https://github.com/ray-project/kuberay/pull/535), @haoxins)
* Update Helm doc. ([#531](https://github.com/ray-project/kuberay/pull/531), @DmitriGekhtman)
* Failure happened when install operator with kubectl apply ([#525](https://github.com/ray-project/kuberay/pull/525), @kevin85421)
* fix examples: bad K8s log config causing logs to be lost ([#501](https://github.com/ray-project/kuberay/pull/501), @davidxia)
* Helm instructions: kubectl apply -> kubectl create ([#505](https://github.com/ray-project/kuberay/pull/505), @DmitriGekhtman)
* apiserver add new api docs ([#498](https://github.com/ray-project/kuberay/pull/498), @scarlet25151)

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
