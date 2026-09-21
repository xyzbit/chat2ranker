# Rank runtime deployment root

English | [中文](README.zh.md)

This private manifest supplies the shared Node dependency closure for Rank's control host and Execution's DSH adapter. It contains no scheduler or server implementation. The existing applications own behavior; the deployment image selects the process role.

Required workspace peer packages are explicit dependencies so a deployed runtime resolves them without the development checkout. The [Kubernetes lab](../../rank/deploy/k8s/README.md) owns image construction and deployment. Platform-specific native dependencies must be checked inside the target Linux image; a successful host build alone is insufficient.
