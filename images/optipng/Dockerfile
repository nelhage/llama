FROM nelhage/llama as llama

FROM debian:stable
RUN apt-get update && apt-get -y install optipng ca-certificates && apt-get clean
RUN mkdir /var/task
COPY --from=llama /llama_runtime /llama_runtime
ENTRYPOINT ["/llama_runtime"]
CMD /usr/bin/optipng
