#!/usr/bin/env bash

scriptpath=`dirname $0`
srcpath=`dirname $scriptpath`
echo $scriptpath
cd $srcpath

echo "Building from:"
pwd
echo "With GOPATH:"
echo $GOPATH
go version
go env

if [ "$1" = "build" ]; then
echo "==> building k8s-endpoints-sync-controller binary"
[ -e ./dist/k8s-endpoints-sync-controller ] && rm ./dist/k8s-endpoints-sync-controller
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o ./dist/k8s-endpoints-sync-controller src/main/main.go
echo "==> Results:"
echo "==>./dist"
ls ./dist/k8s-endpoints-sync-controller
exit
fi

if [ "$1" = "buildimage" ]; then
echo "==> building k8s-endpoints-sync-controller docker image"
echo "tag: $2"
docker build -t $2 -f Dockerfile .
fi

if [ "$1" = "pushimage" ]; then
echo "==> pushing k8s-endpoints-sync-controller docker image $2 to registry"
tag=$2
ext_tag=$3
docker tag $tag $ext_tag
docker push $ext_tag
fi
