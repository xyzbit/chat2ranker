# Product assets

English | [中文](README.zh.md)

`control/` contains the Rank experiment Skill and Control DSH preset source. `judge/` contains versioned default Judge configurations.

Tested agent configurations are business data. A run freezes an `AgentVersion` snapshot and sends it through the Runner protocol; mutable agent configuration is not loaded directly from this source tree during a run.
