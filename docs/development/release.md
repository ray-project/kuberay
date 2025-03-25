<!-- markdownlint-disable MD013 -->
# KubeRay Release Process

## Prerequisite

You need [KubeRay GitHub](https://github.com/ray-project/kuberay) write permissions to cut a release branch and create a release tag.

### Overview

Each KubeRay minor release series (e.g., `v1.3.X`) is maintained on its own release branch. For example, the `v1.3.X` releases are cut from the `release-1.3` branch. Patch releases (e.g., `v1.3.1`) involve cherry-picking commits onto the corresponding release branch.

The high-level steps for a new minor or patch release are:

1. Create a release branch if one doesn't exist. The release branch name should be based on the major and minor versions (e.g. `release-1.4`).
2. Update the KubeRay version in operator and helm charts. See [this PR](https://github.com/ray-project/kuberay/pull/3071) as an example.
3. Create and push a new git tag based on the release version (e.g. `v1.4.0`).
4. Publish images by running the [release-image-build](https://github.com/ray-project/kuberay/actions/workflows/image-release.yaml) workflow using the release tag as input.
5. Publish kubectl plugin binaries
6. Update `kuberay-helm` repo to publish new versions of the KubeRay helm charts.
7. Validate the release.
8. Generate the release CHANGELOG.
9. Publish release notes.
10. Update ray.io documentation.

See the next section for more details on each step.

### Steps

#### Step 1: Create a release branch

Check your git remote references. In this example we assume `upstream` refers to the official KubeRay repo:
```bash
$ git remote -v
upstream	git@github.com:ray-project/kuberay.git (fetch)
upstream	git@github.com:ray-project/kuberay.git (push)
```

Ensure your local master branch is up to date:
```bash
git pull --rebase
```

Cut the release branch based on the release version. For example, if the release version is `v1.4.0`, the release branch should be `release-1.4`:
```bash
git checkout -b release-1.4 upstream/master
```

Push the release branch:
```bash
git push upstream release-1.4
```

To avoid accidentally pulling commits in `master` to the release branch, set the upstream tracking branch to the newly created release branch:
```bash
git branch --set-upstream-to upstream/release-1.4
```

Github workflows run on `master` and all `release-*` branches. Check that all workflows are passing on the new release branch.

#### Step 2: Update KubeRay version references

Next update references to the KubeRay version. The following files need to be updated:
* helm-chart/kuberay-apiserver/Chart.yaml
* helm-chart/kuberay-apiserver/values.yaml
* helm-chart/kuberay-operator/Chart.yaml
* helm-chart/kuberay-operator/values.yaml
* helm-chart/ray-cluster/Chart.yaml
* ray-operator/config/default/kustomization.yaml
* ray-operator/controllers/ray/utils/constant.go

See [this PR](https://github.com/ray-project/kuberay/pull/3071/files) for an example.

#### Step 3: Create and push the git tag

Ensure you are on the latest version of the release branch
```bash
git checkout release-1.4
git pull --rebase upstream/release-1.4
```

As a sanity check, check that the latest local commit matches the latest remote commit:
```bash
git show
```

Next, create and push the git tag:
```bash
git tag v1.4.0
git push upstream v1.4.0
```

#### Step 4: Publish release images

Trigger the [release-image-build](https://github.com/ray-project/kuberay/actions/workflows/image-release.yaml) workflow to build and publish release images.

To trigger the workflow:
* Click "Run workflow"
* Under "Use workflow from", set the git reference to the release tag created in the previous step (e.g. v1.4.0)
* Under "Commit reference", set the commit to the latest commit in the release branch
* Under "Desired release version", set this to the release tag (e.g. v1.4.0)

Here's an example: 

![github-workflow](github-workflow-example.png)

#### Step 5: Publish kubectl plugin release

Trigger the [release-kubectl-plugin](https://github.com/ray-project/kuberay/actions/workflows/kubectl-plugin-release.yaml) workflow to build and publish the kubectl plugin binary.

To trigger the workflow:
* Click "Run workflow"
* Under "Use workflow from", set the git reference to the release tag (e.g. v1.4.0)

Once completed, the github release should be updated to include the kubectl plugin binary and a PR should be opened in the krew repo.

#### Step 6: Publish KubeRay helm charts

The [kuberay-helm](https://github.com/ray-project/kuberay-helm) repo automatically publishes helm charts when new versions are detected. This repo uses release
branches with identical branch names as the KubeRay repo.

First create a release branch in the kuberay-helm repo:
```
cd kuberay-helm
git checkout -b release-1.4 upstream/main
git push upstream release-1.4
```

Note that creating a new release branch will trigger workflows to publish new helm charts.
However, these workflows should not publish any images since no new versions are being introduced.

To publish a new version, create a release branch and sync the helm charts in the KubeRay repo with kuberay-helm:
```
# in kuberay repo, ensure you are on latest version of the release branch
cd kuberay
git checkout release-1.4
git pull --rebase upstream/release-1.4

# in kuberay-helm, copy helm charts from kuberay repo. This assumes kuberay and kuberay-helm repo share the same parent directory.
cd kuberay-helm
# Remove old charts first to handle deleted files
rm -rf helm-chart/
# Copy the updated charts from the kuberay repo
cp -R ../kuberay/helm-chart/ .
# Remove test file not needed in kuberay-helm repo
# (Adjust path if needed)
rm -f helm-chart/script/rbac_test.py
```

Push the changes to your fork and open a PR to the kuberay-helm repo. The PR should look something like [this](https://github.com/ray-project/kuberay-helm/pull/54).
Once the PR is merged, the [chart-release](https://github.com/ray-project/kuberay-helm/actions/workflows/chart-release.yaml) and [pages-build-deployment](https://github.com/ray-project/kuberay-helm/actions/workflows/pages/pages-build-deployment) workflows should be triggered automatically. 
Verify that both workflows succeed. The helm charts are now published.

#### Step 7: Validate the release

At this point, all artifacts for the release should be publically available. You can quickly verify this by installing KubeRay on a Kind cluster:
```bash
kind create cluster
helm repo update
helm install kuberay-operator kuberay/kuberay-operator --version 1.4.0
helm install raycluster kuberay/ray-cluster --version 1.4.0
```

#### Step 8: Generate the CHANGELOG

Follow [Generating the changelog for a release](https://github.com/ray-project/kuberay/blob/master/docs/release/changelog.md) to generate the change log for the new release.

#### Step 9: Update release notes

Go to the [KubeRay release page](https://github.com/ray-project/kuberay/releases) and draft a new release using the new release tag.
Add release notes, including the generated CHANGELOG from the previous step. 

#### Step 10: Update ray.io documentation

Update all references to the KubeRay version in the ray.io documentation.
