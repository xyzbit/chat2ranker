# Rank Web

English | [中文](README.zh.md)

This directory will contain the Control DSH product assembly for chat2ranker.

The application owns only the browser entry point and DSH plugin composition. Experiment state, scheduling, results, and Runner lifecycle belong to the Go Rank service under [`rank/backend/`](../../rank/backend/README.md).

The initial assembly will mount the Rank Control Host plugin, Rank Client UI plugin, the persistent control Session preset, and the Rank experiment Skill. It must not start a tested agent in the Control DSH process.
