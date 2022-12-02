# KubeRay Release Process

## Prerequisite

- [Github](https://github.com/ray-project/kuberay) Write permissions to cut a release tag/branch.
- [Dockerhub](https://hub.docker.com/u/kuberay) Write permissions to push images with pinned tag.

### Release Process

1. Make sure the last commit you want to release past [Go-build-and-test](https://github.com/ray-project/kuberay/actions/workflows/test-job.yaml) workflow.

2. Check out that commit (in this example, we'll use `6214e560`).

3. Depends on what version you want to release,
    - Major or Minor version - Use the GitHub UI to cut a release branch and name the release branch `v{MAJOR}.${MINOR}-branch`
    - Patch version - You don't need to cut release branch on patch version.

4. Tag the image version from `kuberay/operator:6214e560` to `kuberay/operator:v0.2.0` and push to dockerhub.

    ```
    docker tag kuberay/operator:6214e560 kuberay/operator:v0.2.0
    docker push kuberay/operator:v0.2.0

    docker tag kuberay/apiserver:6214e560 kuberay/apiserver:v0.2.0
    docker push kuberay/apiserver:v0.2.0
    ```

5. Build CLI with multi arch support and they will be uploaded as release artifacts in later step.

6. Create a new PR against the release branch to change container image in manifest to point to that commit hash.

    ```
    images:
    - name: kuberay/operator
      newName: kuberay/operator
      newTag: v0.2.0
    ...
    ``` 

    > note: post submit job will always build a new image using the `PULL_BASE_HASH` as image tag.

7. Follow the Helm chart release process.

8. Create a tag and push tag to upstream.

    ```
    git tag v0.2.0
    git push upstream v0.2.0
    ```

9. Run following code and fetch online git commits from last release (v0.1.0) to current release (v0.2.0).

    ```
     git log v0.1.0..v0.2.0 --oneline
    ```

10. Generate release notes and update Github release. See v0.1.0 example [here](https://github.com/ray-project/kuberay/releases/tag/v0.1.0). Please also upload CLI binaries.

11. Send a PR to update [CHANGELOG.md](../../CHANGELOG.md)

### Release Candidates

Before the final release, you may wish to create release candidates for testing.
The first release candidate is tagged as "vX.Y.Z-rc.0". Subsequent candidates are tagged
"vX.Y.Z-rc.1", "vX.Y.Z-rc.2", etc. Each release candidate should be based on the tip of the release branch.

A release candidate is created by
- Making sure CI tests pass for the tip of the release branch (see Step 1 above).
- Making a commit to the release branch with image tags updated to "vX.Y.Z-rc.n" (see Step 6)
- Tagging the commit as "vX.Y.Z-rc.n" (Step 8)
- Tagging and pushing operator and API server images (Step 4)
- Optionally, releasing Helm charts (Step 7)

To correct issues with a release candidate, you may cherry-pick commits from the master branch
into the release branch and form the next release candidate when appropriate.
It is at the discretion of the release manager whether or not to cherry-pick a given commit.

Once you arrive at a stable release candidate, you may form the final release commit, tagged as
"vX.Y.Z", and tag the final operator and API server images.

The steps of generating release notes (Step 10) and updating CHANGELOG.md (Step 11) are only required for the final release.
