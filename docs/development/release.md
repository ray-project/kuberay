# KubeRay Release Process

## Prerequisite

- [Github](https://github.com/ray-project/kuberay) Write permissions to cut a release tag/branch.

### Overview

Each major release (e.g. `0.4`) is managed in its own GitHub branch.
To release KubeRay, cut a release branch (e.g. `release-0.4`) from master and build commits on that branch
until you reach a satisfactory final release commit.

Immediately after cutting the release branch, create a commit for a release candidate (e.g. `0.4.0-rc.0`),
and build the associated artifacts (images and charts).
If further changes need to be made to the release, pick changes from the master branch into the release branch.
Make as many release candidates as necessary until a stable final release commit is reached.
Then build final release artifacts, publish release notes, and announce the release.

### Steps

1. Ensure that the desired master commit is stable.

    - Make sure the last master commit you want to release passes the [Go-build-and-test](https://github.com/ray-project/kuberay/actions/workflows/test-job.yaml) workflow.
    - The KubeRay documentation is up-to-date.
    - All example configurations use the latest released version of Ray.
    - The example configurations work.

   During the KubeRay `0.4.0` release, we used spreadsheets to track [testing](https://anyscaleteam.slack.com/archives/D0412MV3X3J/p1670030376001989) and [documentation updates](https://docs.google.com/spreadsheets/d/1wlTXCWNtQxCUENa0fP2-dV6UYNUhUCix4exiGsep5GQ/edit#gid=0). The bulk of the manual testing was done right before the branch cut. No substantive code changes were made between the time of the branch cut and final release.

   For future releases, we plan to automate more of the relese testing process. Use your best judgement to decide when to test manually while carrying out the release process.
   To track the progress of KubeRay test development, see the [CI label on the KubeRay GitHub](https://github.com/ray-project/kuberay/labels/ci).

2. Depending on whether the release is major, minor, or patch take the following steps.
    - **Major or Minor version** (e.g. `0.4.0` or `1.0.0`). Create a release branch named `release-X.Y`:
    ```
    git checkout -b release-0.4
    git push -u upstream release-0.4
    ```
    - **Patch version** (e.g. `0.4.1`). You don't need to cut a release branch for a patch version. Instead add commits to the release branch.

3. Create a first release candidate (`v0.4.0-rc.0`).

    a. Merge a PR into the release branch updating Helm chart versions and images. For `v0.4.0-rc0`, we did this in two PRs [1](https://github.com/ray-project/kuberay/pull/784/files) [2](https://github.com/ray-project/kuberay/pull/804/files), but it's fine to do it one. Note that [we no longer include appVersion in the Helm charts](https://github.com/ray-project/kuberay/pull/810).
    b. Release `rc0` images using the [release-image-build](https://github.com/ray-project/kuberay/actions/workflows/image-release.yaml) workflow on GitHub actions.
    You will prompted for a commit reference and an image tag. The commit reference should be the SHA of the tip of the release branch. The image tag should be `vX.Y.Z-rc.0`.
    c. Tag the tip of release branch with `vX.Y.Z-rc.0`.
    ```
    git tag v0.4.0-rc.0
    git push upstream v0.4.0-rc.0
    ```
    d. Release rc0 Helm charts following the [instructions](../release/helm-chart.md).
    e. Open a PR into the Ray repo updating the operator version used in the autoscaler integration test. Make any adjustments necessary for the test to pass. [Example](https://github.com/ray-project/ray/pull/30944/files). Make sure the test labelled [kubernetes-operator](https://buildkite.com/ray-project/oss-ci-build-pr/builds/7141#0184ef25-e62c-4dab-9c7e-ddfd583803cd) passes before merging.
    f. Announce the `rc0` release on the KubeRay slack, with deployment instructions. [Example.](https://ray-distributed.slack.com/archives/C02GFQ82JPM/p1670375020308739).

4. If necessary, create more release candidates (`rc1`, `rc2`, ...)
    - Resolve issues with the release branch by cherry picking master commits
into the release branch.
    - When cherry-picking changes, it is best to open a PR against the release branch -- don't push directly to the release branch.
    - When the next release candidate is ready, repeat step 4 above.

5. Create a final release by repeating Step 4 once more using the tag of the release (`vX.Y.Z`) with no `-rc` suffix.

6. Ask @Jeffwan to build and upload CLI binaries for the release.

7. Run following code and fetch online git commits from last release (v0.3.0) to current release (v0.4.0).

    ```
    git log v0.3.0..v0.4.0 --oneline
    ```

8. Write release notes and update the GitHub release. You can use the [v0.3.0 notes](https://github.com/ray-project/kuberay/releases/tag/v0.3.0) as a guide.

9. Send a PR to add the release notes to [CHANGELOG.md](../../CHANGELOG.md).

10. Update KubeRay versions in Ray and KubeRay master. [Ray Example](https://github.com/ray-project/ray/pull/30981), [KubeRay Example](https://github.com/ray-project/kuberay/pull/821).

11. Announce the release on the KubeRay slack!
