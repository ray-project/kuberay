This module is the collector for aliyunoss.

Oss endpoint and oss bucket are read from /var/collector-config/data.

Content in /var/collector-config/data should be in json format, like 
`{"ossBucket": "", "ossEndpoint": ""}`

Set `--runtime-class-name=aliyunoss` to enable this module.

Currently this module can only be used in ack environment. Oidc must be 
enabled for the cluster, and permission for write the oss must be granted.
