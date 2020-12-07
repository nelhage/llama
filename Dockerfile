FROM golang:1.15-alpine
RUN mkdir /src
RUN apk update && apk add ca-certificates && rm -rf /var/cache/apk/*
WORKDIR /src
ADD go.sum go.mod ./
RUN go mod download
ADD . /src
RUN env CGO_ENABLED=0 go build -tags llama.runtime \
                 -o /llama_runtime \
                 ./cmd/llama_runtime/
FROM alpine
COPY --from=0 /llama_runtime /llama_runtime
ENTRYPOINT ["/llama_runtime"]
