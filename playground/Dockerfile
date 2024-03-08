# docker build -t capt-playground .
# docker run -it --rm --network host -v /tmp:/tmp -v /var/run/docker.sock:/var/run/docker.sock -v /var/run/libvirt/libvirt-sock-ro:/var/run/libvirt/libvirt-sock-ro -v /var/run/libvirt/libvirt-sock:/var/run/libvirt/libvirt-sock --name capt-playground capt-playground
FROM alpine

ADD https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.6.1/clusterctl-linux-amd64 /usr/local/bin/clusterctl 
ADD https://github.com/kubernetes-sigs/kind/releases/download/v0.20.0/kind-linux-amd64 /usr/local/bin/kind
ADD https://github.com/cilium/cilium-cli/releases/download/v0.15.20/cilium-linux-amd64.tar.gz /tmp/cilium.tar.gz

RUN apk add virt-install libvirt-client docker-cli kubectl helm && \
    chmod +x /usr/local/bin/clusterctl /usr/local/bin/kind && \
    tar -C /usr/local/bin -xzf /tmp/cilium.tar.gz && \
    rm /tmp/cilium.tar.gz