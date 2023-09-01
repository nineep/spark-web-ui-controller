FROM golang:1.20.0-alpine as builder

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories
RUN apk add --no-cache bash git

ENV GOPROXY=https://goproxy.cn
ENV GO111MODULE=on
WORKDIR ${GOPATH}/src/nineep.com/spark-web-ui-controller/
COPY . ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /usr/bin/spark-web-ui-controller

FROM alpine
COPY --from=builder /usr/bin/spark-web-ui-controller /usr/bin
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories
RUN apk add --no-cache tini

COPY entrypoint.sh /usr/bin/
ENTRYPOINT ["sh","/usr/bin/entrypoint.sh"]
