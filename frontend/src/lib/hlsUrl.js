export function normalizeHlsPlaybackUrl(rawUrl) {
  const value = typeof rawUrl === 'string' ? rawUrl.trim() : '';
  if (!value) {
    return '';
  }

  if (typeof window === 'undefined') {
    return value;
  }

  try {
    const resolved = new URL(value, window.location.href);
    // Browsers block http media on an https page. Returning an empty URL
    // avoids repeated failed loads and lets the UI show a clear hint.
    if (window.location.protocol === 'https:' && resolved.protocol === 'http:') {
      return '';
    }
    return resolved.toString();
  } catch {
    return '';
  }
}
