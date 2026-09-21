"""Apply the pinned Docker provider's optional private-network endpoint mode.

The upstream allocator probes the server's network namespace, which is not the
Docker host when the server runs in a container. Rank uses authenticated server
proxy endpoints and does not need host-port publication. This build patch fails
on upstream drift; it does not replace any application scheduler or API client.
"""
from importlib.metadata import version
from pathlib import Path

import opensandbox_server

if version("opensandbox-server") != "0.2.3":
    raise RuntimeError("Review the private-network patch before upgrading OpenSandbox")
root = Path(opensandbox_server.__file__).parent


def replace(relative, before, after):
    path = root / relative
    source = path.read_text()
    if source.count(before) != 1:
        raise RuntimeError("OpenSandbox source changed: " + relative)
    path.write_text(source.replace(before, after))


replace("config.py", "class DockerConfig(BaseModel):", "class DockerConfig(BaseModel):")
# Keep the upstream class docstring first and add a validated deployment field.
replace("config.py", "    port_range_min: int = Field(",
        '    publish_ports: bool = Field(default=True, description="Publish host ports; disable when clients use private-network server proxy endpoints.")\n\n    port_range_min: int = Field(')
replace("services/docker/docker_service.py",
        "                if self.network_mode != HOST_NETWORK_MODE:\n                    port_bindings = allocate_port_bindings",
        "                if self.network_mode != HOST_NETWORK_MODE and self.app_config.docker.publish_ports:\n                    port_bindings = allocate_port_bindings")
replace("services/docker/docker_service.py", "        self._ensure_network_policy_support(request)",
        '        if request.network_policy and not self.app_config.docker.publish_ports:\n            raise HTTPException(status_code=400, detail={"code": SandboxErrorCodes.INVALID_PARAMETER, "message": "networkPolicy requires docker.publish_ports"})\n        self._ensure_network_policy_support(request)')
replace("services/docker/networking.py", "        if resolve_internal:",
        "        if resolve_internal or not self.app_config.docker.publish_ports:")

# The dedicated Rank image supplies execd and protected PID 1 at build time.
# Docker refuses put_archive against a read-only rootfs, including before start.
replace("config.py", "    publish_ports: bool = Field(", '''    rank_readonly_runtime: bool = Field(default=False, description="Require a preinstalled Rank runtime with bounded writable storage.")
    rank_workspace_mib: int = Field(default=128, ge=1)
    rank_tmp_mib: int = Field(default=32, ge=1)
    rank_shm_mib: int = Field(default=16, ge=1)
    rank_tmpfs_inodes: int = Field(default=8192, ge=128)
    rank_log_mib: int = Field(default=2, ge=1)

    publish_ports: bool = Field(''')
replace("services/docker/docker_service.py", "        self._ensure_secure_access_support(request)", '''        if self.app_config.docker.rank_readonly_runtime:
            if request.volumes or request.extensions or request.network_policy or self.app_config.docker.sandbox_binds:
                raise HTTPException(status_code=400, detail={"code": SandboxErrorCodes.INVALID_PARAMETER, "message": "Rank read-only runtimes reject additional mounts and extensions"})
        self._ensure_secure_access_support(request)''')
replace("services/docker/container_ops.py", "        return image_uri, auth_config", '''        if self.app_config.docker.rank_readonly_runtime:
            image = self.docker_client.images.get(image_uri)
            if image.attrs.get("Config", {}).get("Volumes"):
                raise HTTPException(status_code=400, detail={"code": SandboxErrorCodes.INVALID_PARAMETER, "message": "Rank read-only runtime images cannot declare volumes"})
        return image_uri, auth_config''')
replace("services/docker/container_ops.py", "        if docker_cfg.no_new_privileges:", '''        if docker_cfg.rank_readonly_runtime:
            if not mem_limit:
                raise ValueError("Rank read-only runtime requires a memory limit")
            options = f"rw,nosuid,nodev,nr_inodes={docker_cfg.rank_tmpfs_inodes}"
            host_config_kwargs.update(
                read_only=True,
                memswap_limit=mem_limit,
                tmpfs={
                    "/run/rank": f"{options},size={docker_cfg.rank_workspace_mib}m,mode=0755",
                    "/tmp": f"{options},size={docker_cfg.rank_tmp_mib}m,mode=1777",
                },
                shm_size=docker_cfg.rank_shm_mib * 1024 * 1024,
                log_config={"Type": "local", "Config": {"max-size": f"{docker_cfg.rank_log_mib}m", "max-file": "2"}},
            )
        if docker_cfg.no_new_privileges:''')
replace("services/docker/container_ops.py",
        "            else:\n                self._prepare_sandbox_runtime(container, sandbox_id, runtime_platform)",
        "            elif not self.app_config.docker.rank_readonly_runtime:\n                self._prepare_sandbox_runtime(container, sandbox_id, runtime_platform)")

# Protected PID 1 installs netfilter, then replaces itself with a process whose
# bounding set excludes NET_ADMIN/NET_RAW before starting the command daemon.
replace("services/docker/container_ops.py", "        if docker_cfg.pids_limit is not None:", '''        if docker_cfg.rank_readonly_runtime:
            if network_mode == "host" or network_mode.startswith("container:"):
                raise ValueError("Rank network isolation requires a private container network namespace")
            host_config_kwargs["cap_drop"] = [cap for cap in docker_cfg.drop_capabilities if cap != "NET_ADMIN"]
            host_config_kwargs["cap_add"] = ["NET_ADMIN"]
        if docker_cfg.pids_limit is not None:''')
replace("services/docker/container_ops.py",
        '                    container_kwargs["entrypoint"] = [BOOTSTRAP_PATH]',
        '                    container_kwargs["entrypoint"] = (["/usr/local/bin/execution-sandbox", "-mode", "network-init"] if self.app_config.docker.rank_readonly_runtime else [BOOTSTRAP_PATH])')
