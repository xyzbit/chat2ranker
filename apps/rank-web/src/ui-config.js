/** Shared menu positioning for single-select and searchable model menus. */
export const menuConfig = {
  gap: 6,
  viewportPadding: 12,
  maxHeight: 300,
  minWidth: 240,
};

/** CSS dimensions derived from the same menu positioning values. */
export const menuStyle = {
  "--menu-max-height": `${menuConfig.maxHeight}px`,
  "--menu-min-width": `${menuConfig.minWidth}px`,
  "--menu-viewport-padding": `${menuConfig.viewportPadding}px`,
};
