// Display formatters. Every numeric the UI shows passes through here so units
// are consistent and tabular-mono-friendly (the .num class supplies the font).

const KB = 1000;

/**
 * Human byte size with decimal (SI) units, matching put.io's own GB/TB display
 * convention (the mockup shows "1.0 TB" for a 1e12-byte plan). One decimal for
 * sub-1000 magnitudes, integer bytes below 1 KB.
 */
export function humanBytes(bytes: number): string {
  if (!isFinite(bytes) || bytes < 0) return '0 B';
  if (bytes < KB) return `${Math.round(bytes)} B`;
  const units = ['KB', 'MB', 'GB', 'TB', 'PB'];
  let v = bytes / KB;
  let i = 0;
  while (v >= KB && i < units.length - 1) {
    v /= KB;
    i++;
  }
  return `${v.toFixed(1)} ${units[i]}`;
}

/** Human transfer rate (bytes/sec → "8.4 MB/s"). 0 renders as a dash. */
export function humanRate(bytesPerSec: number): string {
  if (!bytesPerSec || bytesPerSec <= 0) return '—';
  return `${humanBytes(bytesPerSec)}/s`;
}

/** Seconds → "m:ss" / "h:mm:ss". 0/absent → unknown dash. */
export function humanEta(seconds: number): string {
  if (!seconds || seconds <= 0) return '—';
  const s = Math.round(seconds);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}`;
  return `${m}:${String(sec).padStart(2, '0')}`;
}

/** RFC3339 → "HH:MM:SS" local clock for log timestamps. */
export function clockTime(rfc3339: string): string {
  const d = new Date(rfc3339);
  if (isNaN(d.getTime())) return '--:--:--';
  return d.toLocaleTimeString('en-GB', { hour12: false });
}

/** "added 6m ago" style relative age from an RFC3339 timestamp. */
export function relativeAge(rfc3339: string | null, now = Date.now()): string {
  if (!rfc3339) return '';
  const t = new Date(rfc3339).getTime();
  if (isNaN(t)) return '';
  const diff = Math.max(0, Math.round((now - t) / 1000));
  if (diff < 60) return `${diff}s ago`;
  const m = Math.floor(diff / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

/** Render the wall clock for the rail (HH:MM:SS, local). */
export function railClock(now = new Date()): string {
  return now.toLocaleTimeString('en-GB', { hour12: false });
}

/** k=v field rendering for a log line's trailing structured fields. */
export function formatFields(fields: Record<string, unknown>): string {
  const parts: string[] = [];
  for (const [k, v] of Object.entries(fields)) {
    if (v === null || v === undefined) continue;
    parts.push(`${k}=${typeof v === 'object' ? JSON.stringify(v) : String(v)}`);
  }
  return parts.join(' ');
}
