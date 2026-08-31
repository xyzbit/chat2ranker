# Agent Note: Public README and one-minute quick start

Status: implemented

English | [中文](2026-08-30-public-readme-quickstart.zh.md)

## Problem

The root README introduced repository ownership and development architecture before the public launch path, while the local acceptance guide mixed first-time user setup with contributor prerequisites, process topology, and release verification. A new user could not identify the shortest path to a successful first experiment.

## Decision

The bilingual root READMEs lead with the published `npx` command, one outcome-focused description, and real product screenshots. They summarize evaluation behavior and link architecture instead of teaching repository internals before startup.

Dedicated bilingual quick-start tutorials own first-run model setup, the first experiment, result inspection, background operation, and isolated profiles. `rank/docs/local-acceptance.md` owns source startup and contributor acceptance. Root documentation does not include a star-history chart; current product evidence and the launch path take priority over growth decoration.

## Alternatives considered

**Keep one comprehensive guide.** A single file avoids another documentation pair, but it preserves the mixed user and contributor sequence that obscures the first successful run.

**Add release and star-history dashboards to the README.** Badges provide compact release status, while a trend chart consumes prominent space without helping a user start or evaluate an Agent.

**Publish only a documentation-site landing page.** A separate site can add richer navigation later, but GitHub and npm users still need a complete launch path at the repository entry point.

## Consequences

New users can copy one command before reading architecture or source prerequisites. Maintainers must keep both quick-start languages and the two product screenshots aligned with visible onboarding, run-card, and result behavior. Contributor acceptance remains detailed without becoming the public landing page.
