FROM public.ecr.aws/docker/library/node@sha256:2fe369e969550cde8e867afc3fe370b260140cab4a23d467074295b42163d553
ARG BUBBLEWRAP_VERSION=0.8.0-2+deb12u1
RUN apt-get update && apt-get install -y --no-install-recommends bubblewrap=${BUBBLEWRAP_VERSION} && \
    rm -rf /var/lib/apt/lists/*
COPY runtime /opt/rank/runtime
COPY rankd executiond execution-worker /usr/local/bin/
COPY fixtures /opt/rank/fixtures
COPY rank-assets /opt/rank/rank/assets
RUN mkdir -p /opt/rank/apps /opt/rank/rank && \
    ln -s /opt/rank/runtime/node_modules/@deepseek-ai/dsh /opt/rank/apps/cli && \
    ln -s /opt/rank/runtime/node_modules/@deepseek-ai/dsh-execution-runtime /opt/rank/apps/execution-runtime && \
    ln -s /opt/rank/runtime/node_modules/@xyzbit/agentkiln-web /opt/rank/apps/rank-web
ENV EXECUTION_REPO_ROOT=/opt/rank EXECUTION_WORKER_BIN=/usr/local/bin/execution-worker
ENV RANK_REPO_ROOT=/opt/rank DSH_TELEMETRY_DISABLED=1
USER node
WORKDIR /tmp
ENTRYPOINT ["/usr/local/bin/executiond"]
