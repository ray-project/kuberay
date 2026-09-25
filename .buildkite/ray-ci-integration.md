
# Ray CI Integration

| Version                | Latest Ray Release            |  Ray Nightly                                    |
| -----------            | :-------------------          | :--------------                                 |
| Latest KubeRay Release | During Ray & KubeRay Releases | Nightly from Ray Release Automation             |
| KubeRay Nightly        | In KubeRay CI                 | **test-e2e-ray-nightly.yml** (nightly schedule) |

This table lays out the state of testing between Ray and KubeRay nightlies and releases.
The goal is to have all 4 of these being consistently tested eventually.
All tests run in KubeRay CI pipeline, the difference is just where the pipeline is actually kicked off from.
"KubeRay Nightly" just refers to running on master right now, And "Latest KubeRay Release" refers to running
on the latest release branch. The "Latest Ray Release" will be pulled from DockerHub and same with "Ray Nightly".

## Testing KubeRay with Ray Nightly

The `.buildkite/test-e2e-ray-nightly.yml` pipeline runs e2e tests against `rayproject/ray:nightly`.
It should be configured to run on a nightly schedule to catch compatibility issues early.

**How it works:**

- Sets `KUBERAY_TEST_RAY_IMAGE=rayproject/ray:nightly` for ray-operator tests
- Sets `E2E_API_SERVER_RAY_IMAGE=rayproject/ray:nightly-py310` for apiserver tests
- Tests that use static YAML files are transformed at runtime using kustomize to override the Ray image

**To run tests locally with nightly Ray:**

```bash
# Ray-operator tests
cd ray-operator
KUBERAY_TEST_RAY_IMAGE=rayproject/ray:nightly go test -v ./test/e2e/...

# Apiserver tests (requires apiserver deployed with curl sidecar)
cd apiserver
E2E_API_SERVER_RAY_IMAGE=rayproject/ray:nightly-py310 go test -v ./test/e2e/...
```
