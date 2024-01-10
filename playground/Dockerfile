FROM fedora:40

RUN yum install -y python-virtualbmc && yum clean all && rm -rf /var/cache/yum

ENTRYPOINT ["vbmcd", "--foreground"]
