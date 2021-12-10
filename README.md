# KubeRay

[![Build Status](https://github.com/ray-project/kuberay/workflows/Go-build-and-test/badge.svg)](https://github.com/ray-project/kuberay/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/ray-project/kuberay)](https://goreportcard.com/report/github.com/ray-project/kuberay)

KubRay is an open source toolkit to run Ray applications on Kubernetes.

KubeRay provides several tools to improve running and managing Ray's experience on Kubernetes.

- Ray Operator
- Backend services to create/delete cluster resources (incubating)
- Kubectl plugin/CLI to operate CRD objects (future work)
- Kubernetes event dumper for ray clusters/pod/services (future work)
- Operator Integration with Kubernetes node problem detector (future work)
- Kubernetes based workspace to easily submit ray jobs (future work)

## Use helm chart

A helm chart is a collection of files that describe a related set of Kubernetes resources. It can help users to deploy ray-operator and ray clusters conveniently.
Please read [kubray-operator](helm-chart/kubray-operator/README.md) to deploy an operator and [ray-cluster](helm-chart/ray-cluster/README.md) to deploy a custom cluster.

## Development

Please read our [CONTRIBUTING](CONTRIBUTING.md) guide before making a pull request. Refer to our [DEVELOPMENT](./ray-operator/DEVELOPMENT.md) to build and run tests locally.

## Security

If you discover a potential security issue in this project, or think you may
have discovered a security issue, we ask that you notify KubeRay Security via our
[Slack Channel](https://ray-distributed.slack.com/archives/C02GFQ82JPM).
Please do **not** create a public GitHub issue.

## License

This project is licensed under the [Apache-2.0 License](LICENSE).
