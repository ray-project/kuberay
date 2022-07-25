# KubeRay Release Process

## Prerequisite

- [Github](https://github.com/ray-project/kuberay) Write permissions to cut a release tag/branch.
- [Dockerhub](https://hub.docker.com/u/kuberay) Write permissions to push images with pinned tag. 
	
### Release Process

1. Make sure the last commit you want to release past [Go-build-and-test](https://github.com/ray-project/kuberay/actions/workflows/test-job.yaml) workflow.

1. Check out that commit (in this example, we'll use `6214e560`). 

1. Depends on what version you want to release,
    - Major or Minor version - Use the GitHub UI to cut a release branch and name the release branch `v{MAJOR}.${MINOR}-branch`
    - Patch version - You don't need to cut release branch on patch version.

1. Tag the image version from `kuberay/operator:6214e560` to `kuberay/operator:v0.2.0` and push to dockerhub.

    ```
    docker tag kuberay/operator:6214e560 kuberay/operator:v0.2.0
    docker push kuberay/operator:v0.2.0
    
    docker tag kuberay/apiserver:6214e560 kuberay/apiserver:v0.2.0
    docker push kuberay/apiserver:v0.2.0
    ```

1. Build CLI with multi arch support and they will be uploaded as release artifacts in later step.

1. Create a new PR against the release branch to change container image in manifest to point to that commit hash.  

    ```
    images:
    - name: kuberay/operator
      newName: kuberay/operator
      newTag: v0.2.0
    ...
    ``` 

    > note: post submit job will always build a new image using the `PULL_BASE_HASH` as image tag.

1. Create a tag and push tag to upstream.

    ```
    git tag v0.2.0
    git push upstream v0.2.0
    ```

1. Run following code and fetch online git commits from last release (v0.1.0) to current release (v0.2.0).

    ```
     git log v0.1.0..v0.2.0 --oneline
    ```
 
1. Generate release notes and update Github release. See v0.1.0 example [here](https://github.com/ray-project/kuberay/releases/tag/v0.1.0). Please also upload CLI binaries.

1. Send a PR to update [CHANGELOG.md](../../CHANGELOG.md)
 