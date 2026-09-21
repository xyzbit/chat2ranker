# Configure Rank in Conversation

English | [中文](conversation-configuration.zh.md)

Rank conversation supports the same configuration as the platform forms. Plain configuration values can be supplied in a message. Credentials, permission review and browser files use an explicit form opened from the conversation.

| Configuration | Conversation behavior |
| --- | --- |
| Agent runtime, model, instructions, tools and Skills | Create an Agent or an immutable version. |
| Dataset cases, schema, description and rubric | Create a dataset or a new version; omitted fields retain the base values. Replacing cases can edit or remove existing cases. |
| Default development and regression datasets | Update Agent-family defaults independently of Agent versions. |
| Evaluation Agents and repeat count | Prepare a confirmation with the requested versions and count. Starting still requires confirmation. |
| Model connection metadata and prices | Update non-secret values directly; open the exact connection form for credentials and verification. |
| Control and Judge models | Bind a verified connection and model, or open the system-model form. |
| Endpoint limits | Set rate, concurrency and timeout at creation. Limits remain immutable. |
| Publication thresholds | Update supplied numeric fields; preserve omitted values. This does not publish or alter running checks. |
| API Keys | Open the form to create, review permissions or revoke. |
| Local connections | Open discovery and import without replacing a system-model binding. |
| Webhooks | Open configuration to create, enable or disable a subscription. Signing secrets remain in the form. |
| Language and companion | Open browser preferences, including local sprite import. |
| Integration | Open the endpoint's key selection and executable examples. |

Configuration actions are durable conversation events. Clicking one reopens current settings; opening it is not a saved change. Closing a secret form discards its plaintext. Configuration events store section and resource references only, never API keys, signing secrets or copied executable commands. Users do not need to enter internal identifiers or JSON in conversation.

## Examples

- “Create a customer-support Agent using our verified DeepSeek connection.”
- “Change this dataset's rubric and keep its existing cases.”
- “Create endpoint support-test with 30 requests per minute and two concurrent responses.”
- “Set support-test's minimum pass rate to 95%.”
- “Create an API Key” opens a secure form rather than returning a secret in chat.
- “Configure Webhooks for support-test” opens the selected endpoint's configuration.

Provider secrets belong to the Execution Service credential store. Serving keys store a hash and display plaintext once. See [serving authentication](serving-api.md) for API details.
