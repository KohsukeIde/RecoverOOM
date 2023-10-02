FROM golang:1.19-alpine

ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64

WORKDIR /go/src
ADD go.mod go.sum /go/src/

RUN go mod download

RUN apk add --no-cache --virtual=build-deps wget \
  && wget https://storage.googleapis.com/kubernetes-release/release/v1.13.0/bin/linux/amd64/kubectl \
  && mv kubectl /usr/local/bin/kubectl \
  && chmod +x /usr/local/bin/kubectl \
  && apk del build-deps

ADD ./api /go/src/api
ADD ./*.go /go/src
ADD ./cmd /go/src/cmd
RUN go build -o main ./api/

CMD ["./main"]