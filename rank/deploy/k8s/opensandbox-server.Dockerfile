FROM rank-opensandbox-upstream:0.2.3
COPY opensandbox-proxy-only.py /tmp/opensandbox-proxy-only.py
RUN python /tmp/opensandbox-proxy-only.py && rm /tmp/opensandbox-proxy-only.py
ENTRYPOINT ["opensandbox-server", "--config", "/state/server.toml"]
