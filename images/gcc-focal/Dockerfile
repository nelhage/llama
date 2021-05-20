FROM ghcr.io/nelhage/llama as llama
FROM ubuntu:focal
RUN apt-get update && apt-get -y install gcc g++ gcc-9 g++-9 ca-certificates && apt-get clean
COPY --from=llama /llama_runtime /llama_runtime
WORKDIR /
ENTRYPOINT ["/llama_runtime"]
