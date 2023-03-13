# IAM roles for service account on AWS EKS

Applications in a pod's containers can use an AWS SDK or the AWS CLI to make API requests to AWS services using AWS Identity and Access Management (IAM) permissions. Applications must sign their AWS API requests with AWS credentials. IAM roles for service accounts provide the ability to manage credentials for your applications. To achieve this, you can read the following articles:

* [IAM roles for service accounts](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html): This is the official AWS documentation that explains IAM roles for service accounts step-by-step.
* [Understanding IAM roles for service accounts, IRSA, on AWS EKS.](https://medium.com/@ankit.wal/the-how-of-iam-roles-for-service-accounts-irsa-on-aws-eks-3d76badb8942): Good article to provide an easy-to-understand explanation.

## Pitfall

For example, users may want to download their files from their S3 bucket with AWS Python SDK (`boto3`) in Ray Pods. However, there is a pitfall in the Ray images. When you execute the **boto3_example_1.py** in a Ray Pod, you will get an error like `An error occurred (403) when calling the HeadObject operation: Forbidden`.

```python
# boto3_example_1.py
import boto3

s3 = boto3.client('s3')
bucket_name = YOUR_BUCKET_NAME
key = YOUR_OBJECT_KEY
filename = YOUR_FILENAME

s3.download_file(bucket_name, key, filename)
```

The root cause is that the version of `boto3` in the Ray image is too old. To elaborate, `rayproject/ray:2.3.0` provides boto3 version 1.4.8 (Nov. 21, 2017),
a more recent version (1.26) is currently available as per https://pypi.org/project/boto3/#history. The boto3 1.4.8 does not support to initialize the security credentials automatically in some cases (e.g. `AssumeRoleWithWebIdentity`). 

```shell
# image: rayproject/ray:2.3.0
pip freeze | grep boto
# boto3==1.4.8
# botocore==1.8.50
```

## Workaround solutions
### Solution 1: Setup the credentials explicitly
```python
# boto3_example_2.py
import os
import boto3
def assumed_role_session():
    role_arn = os.getenv('AWS_ROLE_ARN')
    with open(os.getenv("AWS_WEB_IDENTITY_TOKEN_FILE"), 'r') as content_file:
        web_identity_token = content_file.read()
    role = boto3.client('sts').assume_role_with_web_identity(RoleArn=role_arn, RoleSessionName='assume-role',
                                                             WebIdentityToken=web_identity_token)
    credentials = role['Credentials']
    aws_access_key_id = credentials['AccessKeyId']
    aws_secret_access_key = credentials['SecretAccessKey']
    aws_session_token = credentials['SessionToken']
    return boto3.session.Session(aws_access_key_id=aws_access_key_id, aws_secret_access_key=aws_secret_access_key,
                              aws_session_token=aws_session_token)

session = assumed_role_session()
s3 = session.client("s3")
bucket_name = YOUR_BUCKET_NAME
key = YOUR_OBJECT_KEY
filename = YOUR_FILENAME
s3.download_file(bucket_name, key, filename)
```

### Solution 2: Upgrade the boto3 package
I am not sure whether is there any functionality in Ray depends on boto3. Hence, it may break some dependencies. The progress is tracked in [rayproject/ray#33256](https://github.com/ray-project/ray/issues/33256).

```shell
pip install --upgrade boto3
python3 -m pip install -U pyOpenSSL cryptography
python3 boto3_example_1.py # success
```
