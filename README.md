# llama -- A CLI for outsourcing computation to Amazon Lambda

Llama is a tool for running UNIX commands inside of Amazon Lambda. Its
goal is to make it easy to outsource compute-heavy tasks to Lambda,
with its enormous available parallelism, from your shell.

Lambda is neither the cheapest nor the fastest compute available, but
it has the advantage of supporting nearly-arbitrary parallelism with
no need for provisioned capacity and minimum configuration, making it
very suitable for infrequent burst compute when interactive latency is
desirable.

## An example

The [`optipng`](http://optipng.sourceforge.net/) command compresses
PNG files and otherwise optimizes them to be as small as possible,
typically used in order to save bandwidth and speed load times on
image assets. `optipng` is somewhat computationally expensive and
compressing a large number of PNG files can be a slow operation. With
`llama`, we can optimize a large of images by outsourcing the
computation to lambda.

I prepared a directory full of 151 PNG images of the original Pokémon,
and benchmarked how long it took to optimize them using 8 concurrent
processes on my desktop:


```console
$ time ls -1 *.png | parallel -j 8 optipng {} -out optimized/{/}
[...]
real    0m45.090s
user    5m33.745s
sys     0m0.924s
```

Once we've prepared and `optipng` lambda function (we'll talk about
setup in a later section), we can use `llama` to run the same
computation in AWS Lambda:

```console
$ time ls -1 *.png | llama xargs -logs -j 151 optipng '{{.I .Line}}' -out '{{.O (printf "optimized/%s" .Line)}}'
real    0m16.024s
user    0m2.013s
sys     0m0.569s
```

We use `llama xargs`, which works a bit like `xargs(1)`, but runs each
input line as a separate command in Lambda. It also uses the Go
template language to provide flexibility in substitutions, and offers
the special `.Input` and `.Output` methods (`.I` and `.O` for short)
to mark files to be passed back and forth between the local
environment and Lambda.

Lambda's CPUs are slower than my desktop and the network operations
have overhead, and so we don't see anywhere near a full `151/8`
speedup. However, the additional parallelism still nets us a 3x
improvement in real-time latency. Note also the vastly decreased
`user` time, demonstrating that the CPU-intensive work has been
offloaded, freeing up local compute resources for interactive
applications or other use cases.

This operation consumed about 700 CPU-seconds in Lambda. I configured
`optipng` to have 1792MB of memory, which is the point at which lambda
allocates a full vCPU to the process. That comes out to about 1254400
MB-seconds of usage, or about $0.017 assuming I'm already out of the
Lambda free tier.

# Configuring llama

Llama requires a few resources to be configured in AWS in order to
work.

First of all, we need configured AWS credentials on our development
machine. These should be configured in `~/.aws/credentials` so the AWS
CLI and `llama` both can find them.

Next, we need an S3 bucket and prefix to use as an object store for
moving files back and forth. `llama` expects to find this path in the
`$LLAMA_OBJECT_STORE` environment variable. I'll generate a unique
unique-named bucket here for our example, but you can use an existing
bucket if you have one:

```console
$ LLAMA_BUCKET=llama.$(date +%s)
$ export LLAMA_OBJECT_STORE=s3://$LLAMA_BUCKET/obj/
$ aws s3api create-bucket \
  --bucket $LLAMA_BUCKET \
  --region us-west-2 \
  --create-bucket-configuration LocationConstraint=us-west-2
```

Next, we need an IAM role for our lambdas, with access to CloudFront
(for logging), and our S3 bucket:

```console
$ aws iam create-role --role-name llama --assume-role-policy-document '{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Principal": {
                "Service": "lambda.amazonaws.com"
            },
            "Action": "sts:AssumeRole"
        }
    ]
}'
$ aws iam attach-role-policy \
  --role-name llama \
  --policy-arn arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole
$ aws iam put-role-policy \
  --role-name llama \
  --policy-name llama-access-object-store \
  --policy-document '{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Sid": "LlamaAccessObjectStore",
            "Effect": "Allow",
            "Action": [
                "s3:PutObject",
                "s3:GetObject",
                "s3:ListBucketMultipartUploads",
                "s3:ListBucket"
            ],
            "Resource": [
                "arn:aws:s3:::'$LLAMA_BUCKET'",
                "arn:aws:s3:::'$LLAMA_BUCKET'/*"
            ]
        }
    ]
}
'
```

(If you're using your own bucket, you'll need to modify the s3 grant
accordingly)

Finally, we need to build and install the Llama Lambda runtime as a
Lambda layer. We can do this from this repository using
`scripts/publish-runtime`:

```console
$ layer_arn=$(scripts/publish-runtime)
```

## Packaging functions

Everything above this point only needs to be done once, ever. Now,
however, we're ready to package code into Lambda functions for use
with `llama`. We'll follow these steps for each binary we need to run
using Llama.

Llama supports both old-style Lambda code packages, where the code is
published as a zip file, as well as container images. Container images
are much more flexible, but require an ECR registry and are a bit
fiddly. We'll walk through both approaches.

### Using a container image

To run any code in a container using Llama on Lambda, you just need to
add the Llama runtime to the container, and point the docker
`ENTRYPOINT` at it.

The `Dockerfile` in this repository will build the runtime and create
an image appropriate for use as a base image. Let's build a local
version to make sure we have the latest code:

```console
$ docker build -t nelhage/llama:latest .
```

We can use that image as a base image, if we are willing to use an
Alpine base image. However, we can also just extract the runtime from
it into our image. The `images/optipng/Dockerfile` file builds just such
an image. The key lines there are:

```dockerfile
FROM nelhage/llama as llama
[...]
COPY --from=0 /llama_runtime /llama_runtime
ENTRYPOINT ["/llama_runtime"]
```

In order to deploy a Lambda based on that image, though, we're going
to need an ECR repository. Let's create and configure one:

```console
$ repository_url=$(aws --output text --query repository.repositoryUri ecr create-repository --repository-name llama)
$ aws ecr get-login-password | docker login --username AWS --password-stdin $(dirname "$repository_url")
```

We're now ready to build and upload our image:
```console
$ docker build -t "${repository_url}:optipng" images/optipng/
$ docker push "${repository_url}:optipng"
```

We're now ready to create our function:

```console
$ account_id=$(aws --output text --query Account sts get-caller-identity)
$ aws lambda create-function \
    --function-name optipng \
    --package-type Image \
    --code "ImageUri=${repository_url}:optipng" \
    --timeout 60 \
    --memory-size 1792 \
    --environment "Variables={LLAMA_OBJECT_STORE=$LLAMA_OBJECT_STORE}" \
    --role "arn:aws:iam::${account_id}:role/llama"
```

### Using a zip file

Alternately, we can use an old-style Lambda layer and zip file to
package our code. In this approach, we are responsible for packaging
all of our dependencies. By way of example, we'll just package a small
shell script for lambda. First, we need to make the Llama runtime
available as a Lambda layer:

```console
$ llama_runtime_arn=$(scripts/publish-runtime)
```

Now we can create a zip file containing our code, and publish the
function:

```console
$ zip -o _obj/hello.zip -j images/hello-llama/hello.sh
$ aws lambda create-function \
    --function-name hello \
    --zip-file fileb://_obj/hello.zip \
    --runtime provided.al2 \
    --handler hello.sh \
    --timeout 60 \
    --memory-size 512 \
    --layers "$llama_runtime_arn" \
    --environment "Variables={LLAMA_OBJECT_STORE=$LLAMA_OBJECT_STORE}" \
    --role "arn:aws:iam::${account_id}:role/llama"
```

And invoke it:

```console
$ llama invoke hello world
Hello from Amazon Lambda
Received args: world
```

[parallel]: https://www.gnu.org/software/parallel/

# Cleaning up old objects

Llama uses the S3 bucket as a content-addressable store to move
objects between your workstation and the Lambda worker
processes. Objects are not needed after a llama invocation completes,
so you may optionally wish to establish a bucket lifecycle policy to
clean up old objects:

```console
aws s3api put-bucket-lifecycle-configuration --bucket $LLAMA_BUCKET  --lifecycle-configuration '{
  "Rules": [
    {
      "ID": "Delete old objects",
      "Prefix": "obj/",
      "Status": "Enabled",
      "Expiration": {
          "Days": 31
      }
    }
  ]
}'
```


# Inspiration

Llama is in large part inspired by [`gg`][gg], a tool for outsourcing
builds to Lambda. Llama is a much simpler tool but shares some of the
same ideas and is inspired by a very similar vision of using Lambda as
high-concurrency burst computation for interactive uses.

[gg]: https://github.com/StanfordSNR/gg
