export function modelOptions(templateModels = [], discoveredModels = []) {
  const official = new Map(templateModels.map((item) => [item.id, { id: item.id, name: item.name, source: "official" }]));
  return [...official.values(), ...discoveredModels.filter((id) => id && !official.has(id)).map((id) => ({ id, name: id, source: "discovered" }))];
}

export function isManualModel(value, options) {
  return Boolean(value) && !options.some((item) => item.id === value);
}
