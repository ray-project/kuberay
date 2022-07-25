# Observability

### Monitor

We have added a parameter `--metrics-expose-port=8080` to open the port and expose metrics both for the ray cluster and our control plane. We also leverage the [Prometheus Operator](https://github.com/prometheus-operator/prometheus-operator) to start the whole monitoring system.

You can quickly deploy one by the following on your own kubernetes cluster by using the scripts in install:

```shell
./install/prometheus/install.sh
```
It will set up the prometheus stack and deploy the related service monitor in `config/prometheus`

Then you can also use the json in `config/grafana` to generate the dashboards.
