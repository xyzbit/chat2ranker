# Cross-process tests

English | [中文](README.zh.md)

`contract/` verifies that Go, TypeScript, and Runner implementations agree on protocol fields and terminal states. `e2e/` boots Control DSH, Rank services, and a Mock or DSH Runner through their real process entry points.

Unit tests remain beside their owning Go or TypeScript package. This directory is reserved for behavior that crosses process or language boundaries.
