FROM public.ecr.aws/docker/library/node@sha256:2fe369e969550cde8e867afc3fe370b260140cab4a23d467074295b42163d553
COPY executiond execution-worker /usr/local/bin/
COPY harness.mjs /opt/rank/harness.mjs
USER node
ENV EXECUTION_WORKER_BIN=/usr/local/bin/execution-worker
ENV EXECUTION_HARNESS_DSH_ARGV='["node","/opt/rank/harness.mjs","{prompt}","{workspace}"]'
ENTRYPOINT ["/usr/local/bin/executiond"]
