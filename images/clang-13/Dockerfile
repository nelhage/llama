FROM ghcr.io/nelhage/llama as llama
FROM ubuntu:focal
ENV DEBIAN_FRONTEND noninteractive
RUN apt-get update && apt-get -y install \
        lsb-release wget software-properties-common valgrind zlib1g-dev
ADD https://apt.llvm.org/llvm.sh /tmp/llvm.sh
RUN bash /tmp/llvm.sh 13
RUN update-alternatives --install /usr/bin/cc cc /usr/bin/clang-13 30 && \
    update-alternatives --install /usr/bin/c++ c++ /usr/bin/clang++-13 30
COPY --from=llama /llama_runtime /llama_runtime
WORKDIR /
ENTRYPOINT ["/llama_runtime"]
