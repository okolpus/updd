export const MAX_FILE = 12 * 1024 * 1024;
const FILE_PATTERN = /\.(txt|pdf|epub)$/i;

// Checks a dropped or chosen file before it is sent; returns an error text or ''.
export function fileProblem(file) {
  if (!FILE_PATTERN.test(file.name)) return `«${file.name}»: поддерживаются TXT, PDF и EPUB.`;
  if (file.size > MAX_FILE) return `«${file.name}»: файл больше 12 МБ.`;
  if (!file.size) return `«${file.name}»: файл пустой.`;
  return '';
}

// True when a drag event carries files (not text or links dragged from the page).
export const hasFiles = e => Array.from(e?.dataTransfer?.types || []).includes('Files');
