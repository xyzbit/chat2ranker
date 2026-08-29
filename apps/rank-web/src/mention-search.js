/** @param {unknown} value @returns {string} The normalized active mention query. */
export function mentionSearchQuery(value) {
  return String(value).match(/(?:^|\s)@([^\s@]*)$/)?.[1].toLocaleLowerCase() || "";
}

/** @param {Array} items @param {string} query @param {(item: object) => unknown[]} fields @returns {Array} Matching items in source order. */
export function filterMentionItems(items, query, fields) {
  if (!query) return items;
  const needle = query.toLocaleLowerCase();
  return items.filter((item) => fields(item).some((value) => String(value || "").toLocaleLowerCase().includes(needle)));
}
