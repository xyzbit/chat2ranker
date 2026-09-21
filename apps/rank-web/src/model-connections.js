export function modelOptions(templateModels = [], discoveredModels = []) {
  const official = new Map(templateModels.map((item) => [item.id, { id: item.id, name: item.name, source: "official" }]));
  return [...official.values(), ...discoveredModels.filter((id) => id && !official.has(id)).map((id) => ({ id, name: id, source: "discovered" }))];
}

/** Wire protocols supported by provider connection forms. */
export const protocolOptions = [
  { value: "openai-chat-completions", label: "Chat Completions" },
  { value: "openai-responses", label: "Responses" },
  { value: "anthropic-messages", label: "Anthropic Messages" },
];

/**
 * Provider choices preserve the catalog order and include one custom entry.
 * @param catalog Provider catalog returned by the service.
 * @param customLabel Localized label for the custom provider.
 * @returns Single-select options without a duplicate custom provider.
 */
export function providerOptions(catalog, customLabel) {
  const options = catalog.map(item => ({ value: item.id, label: item.name }));
  return catalog.some(item => item.id === "custom") ? options : [...options, { value: "custom", label: customLabel }];
}
