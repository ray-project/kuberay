# KubeRay Release Process

## Prerequisite

You need [KubeRay GitHub](https://github.com/ray-project/kuberay) write permissions to cut a release branch and create a release tag.

### Overview

Each major release (e.g. `0.4`) is managed in its own GitHub branch.
To release KubeRay, cut a release branch (e.g. `release-0.4`) from master and build commits on that branch
until you reach a satisfactory final release commit.

Immediately after cutting the release branch, create a commit for a release candidate (e.g. `0.4.0-rc.0`),
and build the associated artifacts (images and charts).
If further changes need to be made to the release, pick changes from the master branch into the release branch.
Make as many release candidates as necessary until a stable final release commit is reached.
Then build final release artifacts, publish release notes, and announce the release.

### KubeRay release schedule
KubeRay release plans to synchronize with every two Ray releases. KubeRay v0.5.0 synchronizes with Ray 2.4.0, so v0.6.0 should synchronize with Ray 2.6.0.

* **KubeRay feature freeze**: Two weeks before the official Ray release.
* **KubeRay release**: One week before the official Ray release.
* **Update KubeRay documentation in Ray repository**: Finish before the official Ray release.

### Steps

#### Step 0. KubeRay feature freeze

Ensure the last master commit you want to release passes the [Go-build-and-test](https://github.com/ray-project/kuberay/actions/workflows/test-job.yaml) workflow before feature freeze.

#### Step 1. Ensure that the desired master commit is stable

Ensure that the desired master commit is stable by verifying the following:

* The KubeRay documentation is up-to-date.
* All example configurations use the latest released version of Ray.
* The example configurations work.

During the KubeRay `0.5.0` release, we used spreadsheets to track [manual testing](https://docs.google.com/spreadsheets/d/13q059_lcaKb3BFmOlmZTtOqZPuGPYjRuKPqI1FSpCO8/edit?usp=sharing) and [documentation updates](https://docs.google.com/spreadsheets/d/13q059_lcaKb3BFmOlmZTtOqZPuGPYjRuKPqI1FSpCO8/edit?usp=sharing). Instead of using the latest stable release of KubeRay (i.e., v0.4.0 for the v0.5.0 release process), we should verify the master branch using the following:

* The nightly KubeRay operator Docker image: `kuberay/operator:nightly`.
* The local CRD / YAML / Helm charts.

Open PRs to track the progress of manual testing for documentation, but **avoid merging these PRs** until the  Docker images and Helm charts for v0.5.0 are available
(example PRs: [#997](https://github.com/ray-project/kuberay/pull/997), [#999](https://github.com/ray-project/kuberay/pull/999), [#1004](https://github.com/ray-project/kuberay/pull/1004), [#1012](https://github.com/ray-project/kuberay/pull/1012)).
Bug fix pull requests to fix bugs which found in the documentation testing process **can be merged** (example PR: [#1000](https://github.com/ray-project/kuberay/pull/1000)).

Manual testing can be time-consuming, and to relieve the workload, we plan to add more CI tests. The minimum requirements to move forward are:

   * All example configurations can work with `kuberay/operator:nightly` and the latest release of Ray (i.e. 2.3.0 for KubeRay v0.5.0).
   * Update all version strings in the documents.

#### Step 2. Create a new branch in ray-project/kuberay repository

* Depending on whether the release is for a major, minor, or patch version, take the following steps.
  * **Major or Minor version** (e.g. `0.5.0` or `1.0.0`). Create a release branch named `release-X.Y`:
    ```
    git checkout -b release-0.5
    git push -u upstream release-0.5
    ```
  * **Patch version** (e.g. `0.5.1`). You don't need to cut a release branch for a patch version. Instead add commits to the release branch.

#### Step 3. Create a first release candidate (`v0.5.0-rc.0`).

* Merge a PR into the release branch updating Helm chart versions, Helm chart image tags, and kustomize manifest image tags. For `v0.5.0-rc0`, we did this in [PR #1001](https://github.com/ray-project/kuberay/pull/1001)

* Merge a PR into the release branch to update the `KUBERAY_VERSION` in `constant.go` for telemetry purposes.

* Release `rc0` images using the [release-image-build](https://github.com/ray-project/kuberay/actions/workflows/image-release.yaml) workflow on GitHub actions.
You will be prompted for a commit reference and an image tag. The commit reference should be the SHA of the tip of the release branch. The image tag should be `vX.Y.Z-rc.0`.

* Tag the tip of release branch with `vX.Y.Z-rc.0`.
    ```
    git tag v0.5.0-rc.0
    git push upstream v0.5.0-rc.0
    ```

* The [image release CI pipeline](https://github.com/ray-project/kuberay/blob/master/.github/workflows/image-release.yaml) also publishes the `github.com/ray-project/kuberay/ray-operator@vX.Y.Z-rc.0` Go module. KubeRay has supported Go modules since v0.6.0. Follow these instructions to verify the Go module installation.
    ```sh
    # Install the module. This step is highly possible to fail because the module is not available in the proxy server.
    go install github.com/ray-project/kuberay/ray-operator@v1.0.0-rc.0

    # Make the module available by running the go list command to prompt Go to update its index of modules with information about the module you’re publishing.
    # See https://go.dev/doc/modules/publishing for more details.
    GOPROXY=proxy.golang.org go list -m github.com/ray-project/kuberay/ray-operator@v1.0.0-rc.0
    # [Expected output]: github.com/ray-project/kuberay/ray-operator v1.0.0-rc.0

    # Wait for a while until the URL https://sum.golang.org/lookup/github.com/ray-project/kuberay/ray-operator@vX.Y.Z-rc.0 no longer displays "not found". This may take 15 mins based on my experience.
    go install github.com/ray-project/kuberay/ray-operator@v1.0.0-rc.0

    # Check the module is installed successfully.
    ls $GOPATH/pkg/mod/github.com/ray-project/kuberay/
    # [Expected output]: ray-operator@v1.0.0-rc.0
    ```

* Release rc0 Helm charts following the [instructions](../release/helm-chart.md).

* Open a PR into the Ray repo updating the operator version used in the autoscaler integration test. Make any adjustments necessary for the test to pass ([example](https://github.com/ray-project/ray/pull/40918)). Make sure the test labelled [kubernetes-operator](https://buildkite.com/ray-project/oss-ci-build-pr/builds/17146#01873a69-5ccf-4c71-b06c-ae3a4dd9aecb) passes before merging.

* Open another PR in the Ray repo to update the branch used to kick off tests from the Ray release automation pipeline to test with the nightly (context and step location: <https://github.com/ray-project/ray/pull/51539>).

* Announce the `rc0` release on the KubeRay slack, with deployment instructions ([example](https://ray-distributed.slack.com/archives/C02GFQ82JPM/p1680555251566609)).

#### Step 4. Create more release candidates (`rc1`, `rc2`, ...) if necessary

* Resolve issues with the release branch by cherry-picking master commits into the release branch.
* When cherry-picking changes, it is best to open a PR against the release branch -- don't push directly to the release branch.
* Follow step 3 to create new Docker images and Helm charts for the new release candidate.

#### Step 5. Create a final release

* Create a final release (i.e. v0.5.0) by repeating step 4 once more using the tag of the release (`vX.Y.Z`) with no `-rc` suffix.

#### Step 6. Merge open PRs in step 1 and post-release PRs

Now, we have the Docker images and Helm charts for v0.5.0.

* Merge the pull requests in Step 1 (i.e. [#997](https://github.com/ray-project/kuberay/pull/997), [#999](https://github.com/ray-project/kuberay/pull/999), [#1004](https://github.com/ray-project/kuberay/pull/1004), [#1012](https://github.com/ray-project/kuberay/pull/1012))

* Merge post-release pull requests (example: [#1010](https://github.com/ray-project/kuberay/pull/1010)). See [here](https://github.com/ray-project/kuberay/issues/940) to understand the definition of "post-release" and the compatibility philosophy for KubeRay.



#### Step 7. Update KubeRay documentation in Ray repository.

* Update KubeRay documentation in Ray repository with v0.5.0. Examples for v0.5.0:
    * https://github.com/ray-project/ray/pull/33339
    * https://github.com/ray-project/ray/pull/34178

#### Step 8. Generate release

* Manually trigger the `release-kubectl-plugin` CI job to generate the release.
* Follow the [instructions](../release/changelog.md) to generate release notes and add notes in the GitHub release.

#### Step 9. Announce the release on the KubeRay slack!

* Announce the release on the KubeRay slack ([example](https://ray-distributed.slack.com/archives/C02GFQ82JPM/p1681244150758839))!

#### Step 10. Update CHANGELOG

* Send a PR to add the release notes to [CHANGELOG.md](../../CHANGELOG.md).

#### Step 11. Update and improve this release document!

* Update this document and optimize the release process!
