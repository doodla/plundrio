// REST helper. The dashboard reads everything live via SSE; the only write is
// PUT /api/settings. Errors come back in the contract's single shape
// ({ error: { code, message, field } }) — surfaced to the settings UI.

import type { ApiError, SettingsPutResponse } from '../types';

export class ApiRequestError extends Error {
  code: string;
  field?: string;
  constructor(code: string, message: string, field?: string) {
    super(message);
    this.name = 'ApiRequestError';
    this.code = code;
    this.field = field;
  }
}

export async function putSettings(
  body: Record<string, string | number>,
): Promise<SettingsPutResponse> {
  const res = await fetch('/api/settings', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    let err: ApiError | null = null;
    try {
      err = await res.json();
    } catch {
      // non-JSON error body
    }
    if (err?.error) {
      throw new ApiRequestError(err.error.code, err.error.message, err.error.field);
    }
    throw new ApiRequestError('http_error', `request failed (${res.status})`);
  }
  return res.json();
}
