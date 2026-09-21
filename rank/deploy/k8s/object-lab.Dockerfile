FROM public.ecr.aws/docker/library/node@sha256:2fe369e969550cde8e867afc3fe370b260140cab4a23d467074295b42163d553
COPY weed /usr/local/bin/weed
USER node
ENTRYPOINT ["/usr/local/bin/weed"]
