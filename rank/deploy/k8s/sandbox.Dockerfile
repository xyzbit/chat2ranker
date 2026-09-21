ARG RANK_PLATFORM_IMAGE
ARG EXECD_IMAGE
FROM ${EXECD_IMAGE} AS execd
FROM ${RANK_PLATFORM_IMAGE}
USER root
ARG IPTABLES_VERSION=1.8.9-2
RUN apt-get update && apt-get install -y --no-install-recommends iptables=${IPTABLES_VERSION} && \
    command -v setpriv && rm -rf /var/lib/apt/lists/*
COPY --from=execd /execd /opt/opensandbox/execd
COPY execution-sandbox /usr/local/bin/execution-sandbox
COPY execution-worker /usr/local/bin/execution-worker
COPY sandbox-init.sh /usr/local/lib/rank-sandbox-init.sh
COPY sandbox-init.sh /opt/opensandbox/bootstrap.sh
RUN chmod 0555 /usr/local/bin/execution-sandbox /usr/local/lib/rank-sandbox-init.sh /opt/opensandbox/execd /opt/opensandbox/bootstrap.sh
