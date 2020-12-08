FROM nelhage/llama as llama
FROM ubuntu:focal
RUN apt-get update && apt-get -y install gcc-9 ca-certificates && apt-get clean
COPY --from=llama /llama_runtime /llama_runtime
ENTRYPOINT ["/llama_runtime"]
CMD ["/usr/bin/gcc-9"]
