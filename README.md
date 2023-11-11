# llama -- A CLI for outsourcing computation to AWS Lambda

Llama is a tool for running UNIX commands inside of AWS Lambda. Its
goal is to make it easy to outsource compute-heavy tasks to Lambda,
with its enormous available parallelism, from your shell.

Most notably, llama includes `llamacc`, a drop-in replacement for
`gcc` or `clang` which executes the compilation in the cloud, allowing
for considerable speedups building large C or C++ software projects.

Lambda offers nearly-arbitrary parallelism and burst capacity for
compute, making it, in principle, well-suited as a backend for
interactive tasks that briefly require large amounts of compute. This
idea has been explored in the [ExCamera][excamera] and [gg][gg]
papers, but is not widely accessible at present.

[excamera]: https://www.usenix.org/conference/nsdi17/technical-sessions/presentation/fouladi

## Performance numbers

Here are a few performance results from my testing demonstrating the
current speedups achievable from `llamacc`:

|project|hardware|local build|local time|llamacc build|llamacc time|Approx llamacc cost|
|-------|--------|-----------|----------|-------------|------------|-------------------|
|Linux v5.10 defconfig|Desktop (24-thread Ryzen 9 3900)|`make -j30`|1:06|`make -j100`|0:42|$0.15|
|Linux v5.10 defconfig|Simulated laptop (limited to 4 threads)|`make -j8`|4:56|`make -j100`|1:26|$0.15|
|clang+LLVM, -O0|Desktop (24-thread Ryzen 9 3900)|`ninja -j30`|5:33|`ninja -j400`|1:24|$0.49|

As you can see, Llama is capable of speedups for large builds even on
my large, powerful desktop system, and the advantage is more
pronounced on smaller workstations.

# Getting started

## Dependencies

- A Linux x86_64 machine. Llama only supports that platform for
  now. Cross-compilation should in theory be possible but is not
  implemented.
- The [Go compiler](https://golang.org/dl/). Llama is tested on v1.16
  but older versions may work.
- An [AWS account](https://aws.amazon.com/)

### Install llama

You'll need to install Llama from source. You can run

```
go install github.com/nelhage/llama/cmd/...@latest
```

or clone this repository and run
```
go install ./...
```

If you want to build C++, you'll want to symlink `llamac++` to point
at `llamacc`:

```
ln -nsf llamacc "$(dirname $(which llamacc))/llamac++"
```

### Set up your AWS credentials

Llama needs access to your AWS credentials. You can provide them in
the environment via `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`, but
the recommended approach is to use [`~/.aws/credentials`][aws-creds],
as used by. Llama will read keys out of either.

[aws-creds]: https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-files.html

The account whose credentials you use must have sufficient permissions.  The
following should suffice:

* AmazonEC2ContainerRegistryFullAccess
* AmazonS3FullAccess
* AWSCloudFormationFullAccess
* AWSLambda_FullAccess
* IAMFullAccess

### Configure llama's AWS resources

Llama includes a [CloudFormation][cf] template and a command which
uses it to bootstrap all required resources. You can [read the
template][template] to see what it's going to do.

[cf]: https://aws.amazon.com/cloudformation/
[template]: https://github.com/nelhage/llama/blob/master/cmd/llama/internal/bootstrap/template.json

Once your AWS credentials are ready, run

```
$ llama bootstrap
```

to create
the required AWS resources. By default, it will prompt you for an AWS
region to use; you can avoid the prompt using (e.g.) `llama -region
us-west-2 bootstrap`.

If you get an error like
```
Creating cloudformation stack...
Stack created. Polling until completion...
Stack is in rollback: ROLLBACK_IN_PROGRESS. Something went wrong.
Stack status reason: The following resource(s) failed to create: [Repository, Bucket]. Rollback requested by user.
```

then you can go to the AWS web console, and find the relevant CloudFormation
stack.  The event log should have more useful errors explaining what went
wrong.  You will then need to delete the stack before retrying the bootstrap.

### Set up a GCC image

You'll need to build a container with an appropriate version of GCC for `llamacc` to use.

If you are running Debian or Ubuntu, you can use
`scripts/build-gcc-image` to automatically build a Debian image and
Lambda function matching your local system:

```console
$ scripts/build-gcc-image
```

If you want more control or are running another distribution, you can
look at `images/gcc-focal` for an example Dockerfile to build a
compiler package. You can build that or a similar image into a Lambda
function using `llama update-function` like so:

``` console
$ llama update-function --create --build=images/gcc-focal gcc
```

## Using `llamacc`

To use `llamacc`, run a build using `make` or a similar build system
with a much higher `-j` concurrency than you normally would -- try
5-10x the number of local cores,, and using `llamacc` or `llamac++` as
your compiler. For example, you might invoke

``` console
$ make -j100 CC=llamacc CXX=llamac++
```

## llamacc configuration

`llamacc` takes a number of configuration options from the
environment, so that they're easy to pass through your build
system. The currently supported options include.

|Variable|Meaning|
|--------|-------|
|`LLAMACC_VERBOSE`| Print commands executed by llamacc|
|`LLAMACC_LOCAL`  | Run the compilation locally. Useful for e.g. `CC=llamacc ./configure` |
|`LLAMACC_REMOTE_ASSEMBLE`| Assemble `.S` or `.s` files remotely, as well as C/C++. |
|`LLAMACC_FUNCTION`| Override the name of the lambda function for the compiler|
|`LLAMACC_LOCAL_CC`| Specifies the C compiler to delegate to locally, instead of using 'cc' |
|`LLAMACC_LOCAL_CXX`| Specifies the C++ compiler to delegate to locally, instead of using 'c++' |
|`LLAMACC_LOCAL_PREPROCESS`| Run the preprocessor locally and send preprocessed source text to the cloud, instead of individual headers. Uses less total compute but much more bandwidth; this can easily saturate your uplink on large builds. |
|`LLAMACC_FULL_PREPROCESS`| Run the full preprocessor locally, not just `#include` processing. Disables use of GCC-specific `-fdirectives-only`|
|`LLAMACC_BUILD_ID`| Assigns an ID to the build. Used for Llama's internal tracing support. |
|`LLAMACC_FILTER_WARNINGS`| Filters the given comma-separated list of warnings out of all the compilations, e.g.  `LLAMACC_FILTER_WARNINGS=missing-include-dirs,packed-not-aligned`. |

It is strongly recommended that you use absolute paths if you set
`LLAMACC_LOCAL_CC` and `LLAMACC_LOCAL_CXX`.  Not all build systems will
preserve `$PATH` all the way down to `llamacc`, so if you don't use
absolute paths, you can get build failures that are difficult to diagnose.

# Other features

## `llama invoke`

You can use `llama invoke` to execute individual commands inside of
Lambda. The syntax is `llama invoke <function> <command>
args...`. `<function>` must be the name of a Lambda function using the
Llama runtime. So, for instance, we can inspect the OS running inside
our Lambda image:

``` console
$ llama invoke gcc uname -a
Linux 169.254.248.253 4.14.225-175.364.amzn2.x86_64 #1 SMP Mon Mar 22 22:06:01 UTC 2021 x86_64 x86_64 x86_64 GNU/Linux
```

If your function consumes files as input or output, you can use the
`-f` and `-o` options to specify that files should be passed between
the local and remote nodes. For instance:

``` console
$ llama invoke -f README.md:INPUT -o OUTPUT gcc sh -c 'sha256sum INPUT > OUTPUT'; cat OUTPUT
16c399c108bb783fc5c4529df4fecd0decb81bc0707096ebd981ab2b669fae20  INPUT
```

Note the use of `LOCAL:REMOTE` syntax to optionally specify different
paths between the local and remote ends.

## `llama xargs`

`llama xargs` provides an xargs-like interface for running commands in
parallel in Lambda. Here's an example:

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
$ time ls -1 *.png | llama xargs -logs -j 151 optipng optipng '{{.I .Line}}' -out '{{.O (printf "optimized/%s" .Line)}}'
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

## Managing Llama functions

The llama runtime is designed to make it easy to bridge arbitrary
images into Lambda. You can look at `images/optipng/Dockerfile` in
this repository for a well-commented example explaining how you can
wrap an arbitrary image inside of Lambda for use by Llama.

Once you have a Dockerfile or a Docker image, you can use `llama
update-function` to upload it to ECR and manage the associated Lambda
function. For instance, we could build optipng for the above example
like so:

```console
$ llama update-function --create --build=images/optipng optipng
```

When specifying the memory size for your functions, note that [Lambda
assigns CPU resources to functions based on their memory
allocation](https://docs.aws.amazon.com/lambda/latest/dg/configuration-memory.html). At
1,769 MB, your function will have the equivalent of one full core.

# Other notes

## Inspiration

Llama is in large part inspired by [`gg`][gg], a tool for outsourcing
builds to Lambda. Llama is a much simpler tool but shares some of the
same ideas and is inspired by a very similar vision of using Lambda as
high-concurrency burst computation for interactive uses.

[gg]: https://github.com/StanfordSNR/gg
