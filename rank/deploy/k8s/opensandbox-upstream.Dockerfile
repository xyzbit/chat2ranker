FROM python@sha256:46cb7cc2877e60fbd5e21a9ae6115c30ace7a077b9f8772da879e4590c18c2e3
ARG PYPI_INDEX=https://pypi.org/simple
RUN pip install --no-cache-dir --index-url "${PYPI_INDEX}" opensandbox-server==0.2.3
