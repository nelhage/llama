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
`llama` and [GNU parallel][parallel], we can optimize a large of
images by outsourcing the computation to lambda.

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
$ time ls -1 *.png | parallel -j 151 llama invoke optipng i@{} -out o@optimized/llama-{/}
real    0m21.506s
user    0m9.879s
sys     0m3.086s
```

We use `llama invoke` to run the lambda, and use llama's `i@` and `o@`
annotations on arguments to specify files to be copied between the
local environment and the Lambda environment.

Lambda's CPUs are considerably slower than my desktop and the network
operations have overhead, and so we don't see anywhere near a full
`151/8` speedup. However, the additional parallelism still nets us a
2x improvement in real-time latency. Note also the vastly decreased
`user` time, demonstrating that the CPU-intensive work has been
offloaded, freeing up local compute resources for interactive
applications or other use cases.

This operation consumed about 700 CPU-seconds in Lambda. I configured
`optipng` to have 3008MB of memory (the lambda maximum), because AWS
allocates CPU proportional to memory. That comes out to about 2105600
MB-seconds of usage, or about $0.035 assuming I'm already out of the
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

Now we're ready to create a Lambda function. Llama is not opinionated
about how you package your binaries for Lambda. All it needs is a path
that it can `execve`. For statically-linked binaries you can package
them directly; I've also found it convenient to use `Docker` to
prepare images in the Amazon Linux environment.

To package `optipng`, I've created a `Dockerfile` that will install it
for Amazon Linux and package the necessary dependent `.so` objects
into a zip file. You can run it like so:

```console
$ docker build -t llama_optipng doc/optipng/
$ docker run --rm -v $(pwd):/out llama_optipng cp /optipng.zip /out
```

We're now ready to create our function:

```console
$ account_id=$(aws --output text --query Account sts get-caller-identity)
$ aws lambda create-function \
  --zip-file fileb://optipng.zip \
  --function-name optipng \
  --handler optipng.sh \
  --runtime provided.al2 \
  --memory-size 3008 \
  --role "arn:aws:iam::${account_id}:role/llama" \
  --layers "$layer_arn" \
  --environment "Variables={LLAMA_OBJECT_STORE=$LLAMA_OBJECT_STORE}" \
  --timeout 60
```

[parallel]: https://www.gnu.org/software/parallel/

# Inspiration

Llama is in large part inspired by [`gg`][gg], a tool for outsourcing
builds to Lambda. Llama is a much simpler tool but shares some of the
same ideas and is inspired by a very similar vision of using Lambda as
high-concurrency burst computation for interactive uses.

[gg]: https://github.com/StanfordSNR/gg
