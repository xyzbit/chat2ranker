/** Codex v2 gaze: clockwise from up, eight cells on each of rows 9 and 10. */
export function gazeCell(dx, dy) {
  if (Math.hypot(dx, dy) < 36) return { column: 6, row: 0 };
  const direction = (Math.round(Math.atan2(dx, -dy) / (Math.PI / 8)) + 16) % 16;
  return { column: direction % 8, row: 9 + Math.floor(direction / 8) };
}
