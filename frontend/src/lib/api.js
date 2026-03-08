async function request(path, options = {}) {
  const response = await fetch(path, {
    headers: {
      Accept: 'application/json',
      ...(options.body instanceof FormData ? {} : { 'Content-Type': 'application/json' }),
      ...(options.headers ?? {})
    },
    ...options
  });

  const contentType = response.headers.get('content-type') ?? '';
  const payload = contentType.includes('application/json') ? await response.json() : await response.text();

  if (!response.ok) {
    const message = typeof payload === 'object' && payload?.error ? payload.error : response.statusText;
    throw new Error(message || 'Request failed');
  }

  return payload;
}

export const api = {
  getState() {
    return request('/api/v1/state');
  },
  getRuntimeTexts() {
    return request('/api/v1/runtime/texts');
  },
  saveState(project) {
    return request('/api/v1/state', {
      method: 'PUT',
      body: JSON.stringify(project)
    });
  },
  createSource(source) {
    return request('/api/v1/sources', {
      method: 'POST',
      body: JSON.stringify(source)
    });
  },
  updateSource(source) {
    return request(`/api/v1/sources/${source.id}`, {
      method: 'PUT',
      body: JSON.stringify(source)
    });
  },
  deleteSource(sourceId) {
    return request(`/api/v1/sources/${sourceId}`, {
      method: 'DELETE'
    });
  },
  uploadAsset(kind, file) {
    const formData = new FormData();
    formData.append('file', file);
    return request(`/api/v1/assets/${kind}`, {
      method: 'POST',
      body: formData
    });
  },
  startStream() {
    return request('/api/v1/stream/start', {
      method: 'POST'
    });
  },
  stopStream() {
    return request('/api/v1/stream/stop', {
      method: 'POST'
    });
  }
};
