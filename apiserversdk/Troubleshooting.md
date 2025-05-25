# Troubleshooting

This page contains common issues and their solutions for using the APIServer.

## Getting "Unauthorized" when sending a request

If you receive an "Unauthorized" error when making a request, and you installed APIServer
using the Helm chart, try one of the following solutions

- Install the APIServer without a security proxy. Refer to the [Installation](./Installation.md) guide.
- Add an authorization header to the request: `--header 'Authorization: 12345'`.
