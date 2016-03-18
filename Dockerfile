FROM alpine:3.3
MAINTAINER chengtiesheng <chengtiesheng@huawei.com>

RUN apk add --no-cache \
		bash \
		iptables \
		openssh \
		git \
		curl \
		make              

ENV DIND_COMMIT=b8bed8832b77a478360ae946a69dab5e922b194e DOCKER_VERSION=1.9.1
ADD https://get.docker.com/builds/Linux/x86_64/docker-${DOCKER_VERSION} /usr/bin/docker
ADD https://raw.githubusercontent.com/docker/docker/${DIND_COMMIT}/hack/dind /usr/local/bin/dind
RUN chmod +x /usr/bin/docker /usr/local/bin/dind /usr/local/bin/* && rm -fr /var/lib/docker/*
VOLUME /var/lib/docker

# Store github.com SSH fingerprint
RUN mkdir -p ~/.ssh && ssh-keyscan -H github.com | tee -a ~/.ssh/known_hosts

ENV GIT_CLONE_OPTS="--recursive"

ADD version_list /
ADD *.sh /
COPY builder /usr/local/bin/builder

ENTRYPOINT ["/usr/local/bin/dind", "/run.sh"]
