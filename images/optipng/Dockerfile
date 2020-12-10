# We're going to get the llama runtime from a pre-built
# container. nelhage/llama is built out of the `Dockerfile` in the
# root of the llama repository.
#
# We could build our entire image on top of the llama image, but we
# only need the `llama_runtime` binary, so we're going to copy that
# binary out, which lets us build our image on an arbitrary base
# image.
FROM nelhage/llama as llama

FROM debian:stable

# These lines make Llama work. We copy the Llama runtime, and we point
# `ENTRYPOINT` at it.
COPY --from=llama /llama_runtime /llama_runtime
ENTRYPOINT ["/llama_runtime"]

# Install the packages we need. It's important that we also grab
# `ca-certificates` so the Llama can find a CA store to talk to S3.
RUN apt-get update && \
        apt-get -y install optipng ca-certificates && \
        apt-get clean

# If we specify a CMD, llama will respect it, in a very similar manner
# to `docker run`; without the CMD, any command-line to `llama invoke`
# will be executed directly, using the first argument as the command
# to run.
