FROM nelhage/llama as llama
FROM ubuntu:focal
RUN apt-get update && \
  env DEBIAN_FRONTEND=noninteractive apt-get -y install build-essential ca-certificates git cmake curl && \
  apt-get clean
RUN curl -Lo /tmp/go.tgz https://golang.org/dl/go1.16beta1.linux-amd64.tar.gz && \
  tar -C /usr/local -xzf /tmp/go.tgz && \
  rm /tmp/go.tgz
RUN ln -nsf /usr/local/go/bin /usr/bin/go
RUN mkdir /src
RUN git clone https://github.com/google/boringssl /src/boringssl
COPY --from=llama /llama_runtime /llama_runtime
ENTRYPOINT ["/llama_runtime"]
