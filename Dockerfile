FROM alpine:3.4
RUN apk --no-cache add ca-certificates
ADD kube-dns-manager /kube-dns-manager
ENTRYPOINT ["/kube-dns-manager"]