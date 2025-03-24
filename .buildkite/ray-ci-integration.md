
# Ray CI Integration

| Version                | Latest Ray Release            |  Ray Nightly                         |
| -----------            | :-------------------          | :--------------                      |
| Latest KubeRay Release | During Ray & KubeRay Releases | Nightly from Ray Release Automation  |
| KubeRay Nightly        | In KubeRay CI                 | Not tested                           |

This table lays out the state of testing between Ray and KubeRay nightlies and releases.
The goal is to have all 4 of these being consistently tested eventually.

In the future, if we have a test needs the ray nightly to run, add a step to .buildkite/test-e2e.yaml that
follows the other steps but sets KUBERAY_TEST_RAY_IMAGE env variable to "rayproject/ray:nightly".
When ray releases a new version, you can change the step to just use the latest ray release.
