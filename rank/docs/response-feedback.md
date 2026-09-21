# Invocation Diagnosis and Evaluation

English | [中文](response-feedback.zh.md)

This tutorial starts with a Response ID from an application and produces a reviewed evaluation case. Use the [integration examples](../examples/responses-client/README.md) to obtain an invocation.

## 1. Locate the invocation

Open Invocation Records and search by Response ID or endpoint name. Status and endpoint filters narrow results. Detail URLs use `#/responses?responseId=<id>` and survive refresh. The Agent link opens the immutable version used for that call; previous-turn and conversation links help reconstruct context.

Inspect input/output, additional instructions, caller tools, failure code and durable events. Tokens and latency describe this invocation. Missing cost means pricing is unavailable, not that execution was free. Operator details exclude binary attachment data and credential material.

## 2. Create a reviewed case

Choose Add to Dataset after the invocation finishes. Select a dataset or name a new one, give the case a title, edit the input to remove sensitive information, and write the expected result independently. The actual output is not a reference answer.

Cases store standalone text. For a continuation, include the required historical facts in that text. Additional instructions, attachments and caller tools are not automatically replayed; incorporate necessary instructions and tool results explicitly. Confirm review before saving. Review is an operator acknowledgement, not automated anonymization.

A new dataset starts at version 1. Appending preserves the existing evaluator, rubric and cases and creates the next version. A stale selection returns `dataset_version_changed`; refresh and select the latest version. A Response already captured in that dataset returns `response_already_captured`. Cases retain source Response, endpoint, Agent version and capture time. Older dataset versions and Run snapshots remain unchanged.

## 3. Evaluate and reuse

Choose Evaluate after saving. Rank opens a run confirmation with the source Agent version and the new dataset version; review repeat policy and Judge configuration before starting. Navigation does not start execution. A failed Judge connection is an evaluation infrastructure failure, not evidence that the candidate answer is wrong.

Set this generic dataset as the Agent's development or regression default when it is useful for future checks. Evaluations and releases prefill the default but allow another dataset. Saved cases link back to their invocation for inspection.

## Operator API

`GET /api/responses` accepts `query`, `conversationId`, `deploymentId`, `status` and `before`. `GET /api/responses/{id}` returns diagnostic details. `POST /api/responses/{id}/dataset-cases` accepts `tenantId`, exactly one of `datasetName` or `baseDatasetVersionId`, and `title`, `input`, `expected`, `reviewed: true`; it returns the created DatasetVersion. These are local operator APIs, not serving-key permissions. Remote hosting requires operator authentication as described in the [Serving API](serving-api.md).
