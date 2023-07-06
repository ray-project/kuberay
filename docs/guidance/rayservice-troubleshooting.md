# RayService troubleshooting

RayService is a Custom Resource Definition (CRD) designed for Ray Serve. In KubeRay, it first creates a RayCluster and then proceeds to 
create Ray Serve applications once the RayCluster is ready. If the issue pertains to the data plane, specifically your Ray Serve scripts 
or Ray Serve configurations (`serveConfigV2`), troubleshooting may be challenging. This section provides some tips to help you debug these
issues.

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

```bash
kubectl exec -it $RAY_POD -n $YOUR_NAMESPACE -- bash
# Check the logs under /tmp/ray/session_latest/logs
```

### Method 4: Check Dashboard

```bash
kubectl port-forward $RAY_POD -n $YOUR_NAMESPACE --address 0.0.0.0 8265:8265
# Check $YOUR_IP:8265 in your browser
```
