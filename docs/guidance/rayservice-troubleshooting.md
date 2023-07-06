# RayService troubleshooting

RayService is a Custom Resource Definition (CRD) designed for Ray Serve. In KubeRay, creating a RayService will first create a RayCluster and then
create Ray Serve applications once the RayCluster is ready. If the issue pertains to the data plane, specifically your Ray Serve scripts 
or Ray Serve configurations (`serveConfigV2`), troubleshooting may be challenging. This section provides some tips to help you debug these issues.

## Observability

### Method 1: Check KubeRay operator's logs for errors

```bash
kubectl logs $KUBERAY_OPERATOR_POD -n $YOUR_NAMESPACE | tee operator-log
```

The above command will redirect the operator's logs to a file called `operator-log`. You can then search for errors in the file.

### Method 2: Check RayService CR status

```bash
kubectl describe rayservice $RAYSERVICE_NAME -n $YOUR_NAMESPACE
```

You can check the status and events of the RayService CR to see if there are any errors.

### Method 3: Check logs of Ray Pods
You can also check the Ray Serve logs directly by accessing the log files on the pods. These log files contain system level logs from the Serve controller and HTTP proxy as well as access logs and user-level logs. See [Ray Serve Logging](https://docs.ray.io/en/latest/serve/production-guide/monitoring.html#ray-logging) and [Ray Logging](https://docs.ray.io/en/latest/ray-observability/user-guides/configure-logging.html#configure-logging) for more details.
```bash
kubectl exec -it $RAY_POD -n $YOUR_NAMESPACE -- bash
# Check the logs under /tmp/ray/session_latest/logs/serve/
```

### Method 4: Check Dashboard

```bash
kubectl port-forward $RAY_POD -n $YOUR_NAMESPACE --address 0.0.0.0 8265:8265
# Check $YOUR_IP:8265 in your browser
```

### Common issues

#### Issue 1: Ray Serve script is incorrect.

We strongly recommend that you test your Ray Serve script locally or in a RayCluster before
deploying it to a RayService. [TODO: https://github.com/ray-project/kuberay/issues/1176]

#### Issue 2: `serveConfigV2` is incorrect.

For the sake of flexibility, we have set `serveConfigV2` as a YAML multi-line string in the RayService CR.
This implies that there is no strict type checking for the Ray Serve configurations in `serveConfigV2` field.
Some tips to help you debug the `serveConfigV2` field:

* Check [the documentation](https://docs.ray.io/en/latest/serve/api/#put-api-serve-applications) for the schema about
the Ray Serve Multi-application API `PUT "/api/serve/applications/"`.
* Unlike `serveConfig`, `serveConfigV2` adheres to the snake case naming convention. For example, `numReplicas` is used in `serveConfig`, while `num_replicas` is used in `serveConfigV2`. 

#### Issue 3: The Ray image does not include the required dependencies.

You have two options to resolve this issue:

* Build your own Ray image with the required dependencies.
* Specify the required dependencies via `runtime_env` in `serveConfigV2` field.
  * For example, the MobileNet example requires `python-multipart`, which is not included in the Ray image `rayproject/ray-ml:2.5.0`.
Therefore, the YAML file includes `python-multipart` in the runtime environment. For more details, refer to [the MobileNet example](mobilenet-rayservice.md).

#### Issue 4: Incorrect `import_path`.

The format of the `import_path` is not very straightforward. You can refer to [the documentation](https://docs.ray.io/en/latest/serve/api/doc/ray.serve.schema.ServeApplicationSchema.html#ray.serve.schema.ServeApplicationSchema.import_path) for more details.
Taking [the MobileNet YAML file](../../ray-operator/config/samples/ray-service.mobilenet.yaml) as an example,
the `import_path` is `mobilenet.mobilenet:app`. The first `mobilenet` is the name of the directory in the `working_dir`,
the second `mobilenet` is the name of the Python file in the directory `mobilenet/`,
and `app` is the name of the variable representing Ray Serve application within the Python file.

```yaml
  serveConfigV2: |
    applications:
      - name: mobilenet
        import_path: mobilenet.mobilenet:app
        runtime_env:
          working_dir: "https://github.com/ray-project/serve_config_examples/archive/b393e77bbd6aba0881e3d94c05f968f05a387b96.zip"
          pip: ["python-multipart==0.0.6"]
```