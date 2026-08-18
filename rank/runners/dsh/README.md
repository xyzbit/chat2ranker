# DSH Runner

English | [中文](README.zh.md)

The DSH Runner starts a tested DSH runtime inside the assigned Sandbox.

The wrapper constructs a case-specific DSH home and workspace, loads the frozen agent configuration, creates the tested Agent inside that Runner process, executes one case, and converts the native Session log and usage records into Rank events and artifacts.

The wrapper may drive DSH through its CLI or SDK. Either mechanism remains inside the Runner process and cannot attach to the Control DSH Cordis context.
