# Rank packages

English | [中文](README.zh.md)

This group owns DSH extensions used only by the chat2ranker Control DSH application.

Planned packages are `protocol`, `control-host`, and `client-ui`. They connect the DSH conversation runtime to the Go Rank API, expose Rank tools and A2UI, and project durable `rank/*` control-session events. They never execute a tested harness in the Control DSH process.

No package is created until its first consumer and executable composition exist. Every package follows the repository package rules and depends on DSH Service Definitions rather than concrete providers.
