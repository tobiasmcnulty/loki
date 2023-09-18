FROM golang:1.20.8 as build
ENV BUILD_IN_CONTAINER=false
#COPY . /src/loki
#WORKDIR /src/loki

COPY . /src/bloom-tester
WORKDIR /src/bloom-tester

#RUN go version

#RUN apk update && apk upgrade && \
#    apk add --no-cache bash git

#RUN go mod download
#RUN go build -o ./bloom-tester
#RUN make clean && make BUILD_IN_CONTAINER=false bloom-tester
#RUN make clean
#RUN echo $PWD
#RUN echo "Variable PWD = [$PWD]"
RUN make bloom-tester
#RUN GOROOT=. go build ./bloom-tester
#RUN BUILD_IN_CONTAINER=true CGO_ENABLED=0 GOOS=linux GOROOT=. go build -o bloom-tester

FROM alpine:3.18.3
RUN apk add --update --no-cache ca-certificates
COPY --from=build /src/bloom-tester/tools/tsdb/bloom-tester/bloom-tester /usr/bin/bloom-tester
#ENTRYPOINT [ "/usr/bin/bloom-tester" ]
CMD tail -f /dev/null
