FROM public.ecr.aws/docker/library/nginx@sha256:a8b39bd9cf0f83869a2162827a0caf6137ddf759d50a171451b335cecc87d236
COPY runtime/node_modules/@xyzbit/agentkiln-web/dist/client /usr/share/nginx/html
COPY gateway.conf /etc/nginx/nginx.conf
USER nginx
ENTRYPOINT ["nginx", "-g", "daemon off;"]
