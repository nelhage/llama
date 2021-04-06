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

## `llamacc`

Llama also includes a compiler frontend, `llamacc`, which is a drop-in
replacement for GCC (or clang), but which outsources the actual
compilation to Amazon Lambda. Coupled with a parallel build process
(e.g. `make -j`), it can speed up compiles versus local builds,
especially on laptops without many cores.

On my Google Pixelbook, with 2 cores and 4 threads, using `llamacc`
and `make -j24` cuts the time to build
[boringssl](https://github.com/google/boringssl) in half compared to local compilation.

`llamacc` is a work in progress and I hope for greater speedups with
some additional work.

# Configuring llama

Llama requires a few resources to be configured in AWS in order to
work. Llama includes a [CloudFormation][cf] template and a command
which uses it to bootstrap all required resources. You can [read the
template][template] to see what it's going to do.

[cf]: https://aws.amazon.com/cloudformation/
[template]: https://github.com/nelhage/llama/blob/master/cmd/llama/internal/bootstrap/template.json

First of all, we need configured AWS credentials on our development
machine. These should be configured in `~/.aws/credentials` so the AWS
CLI and `llama` both can find them.

Once you have those, run `llama bootstrap` to create the required AWS
resources. By default, it will prompt you for an AWS region to use;
you can avoid the prompt using (e.g.) `llama -region us-west-2
bootstrap`.

## Packaging functions

`llama bootstrap` only needs to be run once, ever. Once it is
successful, we're ready to package code into Lambda functions for use
with `llama`. We'll follow these steps for each environment we want to
run code in using Llama.

Llama supports old-type Lambda code packages, where the code is
distributed as a zip file, but the easiest way to use Llama is with
Lambda's new Docker container support. Llama can be seen as a bridge
between the Lambda API and Docker containers, allowing us to invoke
arbitrary UNIX command lines within a container.

### Building and uploading a container image

To run any code in a container using Llama on Lambda, you just need to
add the Llama runtime to the container, and point the docker
`ENTRYPOINT` at it. The `images/optipng/Dockerfile` contains a minimal
example, used to create the container for the `optipng` demo
above. It's well-commented and explains the pattern you need to wrap
an arbitrary image inside of Llama.

We can build that `optipng` container and publish it as a Lambda
function using `llama update-function`:

```console
$ llama update-function --create --build=images/optipng optipng
```

We're now ready to `llama invoke optipng`. Try it out:

```console
$ llama invoke optipng optipng --help
```

# llamacc

Llama ships with a `llamac` program that uses `llama` to execute the
actual compilation inside of a Lambda. You can think of this as a
[distcc](https://github.com/distcc/distcc) that doesn't require a
dedicated cluster of your own.

To set it up, you'll need a Lambda function containing an appropriate
llama-compatible GCC. You can build one using Ubuntu Focal's GCC
package using `images/gcc-focal` in this repository contains , or copy
the pattern there if you need a different GCC version or base
OS. Build and upload it like so:

```
$ llama update-function --create --build=images/gcc-focal gcc
```

And now you can use `llamacc` to compile code, just like `gcc`, except
that the compilation happens in the cloud!


```console
$ cat > main.c
#include <stdio.h>

int main(void) {
  printf("Hello, World.\n");
  return 0;
}
$ export LLAMACC_VERBOSE=1; llamacc -c main.c -o main.o && llamacc main.o -o main
2020/12/10 10:43:16 run cpp: ["gcc" "-E" "-o" "-" "main.c"]
2020/12/10 10:43:16 run gcc: ["llama" "invoke" "-o" "main.o" "-stdin" "gcc" "gcc" "-c" "-x" "cpp-output" "-o" "main.o" "-"]
2020/12/10 10:43:17 [llamacc] compiling locally: no supported input detected (["llamacc" "main.o" "-o" "main"])
```

We use `LLAMACC_VERBOSE` to make `llamacc` show what it's doing. We
can see that it runs `cpp` locally to preprocess the given source, and
then invokes `llama` to do the actual compilation in the
cloud. Finally, it transpaerntly runs the link step locally.

Because `llamacc` uses the classic `distcc` strategy of running the
preprocessor locally it is somewhat limited in its scalability, but it
can still get a significant speedup on large projects or on laptops
with slow CPUs with limited cores.

You can also compile C++ by symlinking `llamac++` to `llamacc`.

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
|`LLAMACC_FULL_PREPROCESS`| Run the full preprocessor locally, not just `#include` processing. Disables use of GCC-specific `-fdirectives-only`|

# Other notes

## Using a zip file


Llama also supports packaging code using an old-style Lambda layer and
zip file for code. In this approach, we are responsible for packaging
all of our dependencies.

By way of example, we'll just package a small shell script for
lambda. First, we need to make the Llama runtime available as a Lambda
layer:

```console
$ llama_runtime_arn=$(scripts/dev/publish-runtime)
```

Now we can create a zip file containing our code, and publish the
function:

```console
$ mkdir _obj
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

# Inspiration

Llama is in large part inspired by [`gg`][gg], a tool for outsourcing
builds to Lambda. Llama is a much simpler tool but shares some of the
same ideas and is inspired by a very similar vision of using Lambda as
high-concurrency burst computation for interactive uses.

[gg]: https://github.com/StanfordSNR/gg
