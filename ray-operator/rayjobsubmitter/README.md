# Ray Job Submitter

This is a Go Ray Job Submitter for KubeRay to submit a Ray Job
and tail its logs without installing Ray which is very large.

Note that this tool is designed specifically for KubeRay and
will not support some `ray job submit` features that people
don't use with KubeRay, for example, uploading local files to
a Ray cluster will not be supported by this tool.

## Testing

Tests are located at [../test/e2erayjobsubmitter](../test/e2erayjobsubmitter).

As the e2e suggests, you need to have `ray` installed for these tests
because they need to start a real Ray Head. You can run the tests with:

```sh
make test-e2erayjobsubmitter
```
or GitHub Action: [../../.github/workflows/e2e-tests-ray-job-submitter.yaml](../../.github/workflows/e2e-tests-ray-job-submitter.yaml)
