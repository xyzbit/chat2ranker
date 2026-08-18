# Deployment

English | [中文](README.zh.md)

Local development will use Compose for Control DSH, `rankd`, `rank-worker`, PostgreSQL, and artifact storage. Production deployment will assign one case to one isolated Sandbox and keep one writer for every DSH Session store.

Control DSH, `rankd`, and Runner workloads use separate persistent resources and credentials. Raw DSH Web access must sit behind product authentication and a reverse proxy; DSH trusted-host checks are not an authentication layer.
