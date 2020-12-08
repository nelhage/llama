FROM nelhage/llama as llama
FROM ubuntu:focal
RUN apt-get update && \
  env DEBIAN_FRONTEND=noninteractive apt-get -y install build-essential ca-certificates git cmake curl && \
  apt-get clean
RUN curl -Lo /tmp/go.tgz https://golang.org/dl/go1.15.6.linux-amd64.tar.gz && \
  tar -C /usr/local -xzf /tmp/go.tgz && \
  rm /tmp/go.tgz
RUN mkdir /src
RUN git clone https://go.googlesource.com/go /src/go
WORKDIR /src/go/src
RUN env PATH=$PATH:/usr/local/go/bin ./make.bash
RUN ln -nsf /src/go/bin/go /usr/bin/go
RUN git clone https://github.com/google/boringssl /src/boringssl
COPY --from=llama /llama_runtime /llama_runtime
ENTRYPOINT ["/llama_runtime"]
CMD ["/bin/sh", "-c"]
