#!/usr/bin/env bash

GOARCH=amd64 GOOS=linux CGO_ENABLED=0 go build -o kube-dns-manager .
docker build -t brickchain/kube-dns-manager:latest .
docker push brickchain/kube-dns-manager:latest