// Wire types — mirror plan/contract.md exactly. The frozen contract is the
// source of truth; these are a faithful TS transcription of the Go DTOs in
// internal/dashboard (render.go, rest.go, settings.go, sse.go).

export interface PutioPhase {
  percent_done: number; // 0–100 int (raw put.io)
  downloaded: number; // int64 bytes
  rate_download: number; // bytes/sec
  eta_seconds: number; // int64 sec
}

export interface LocalPhase {
  downloaded: number; // int64 bytes
  total: number; // int64 bytes
  completed_files: number;
  failed_files: number;
  total_files: number;
  rate_download: number; // int64 bytes/sec
  eta_seconds: number; // int64 sec
}

export type LifecycleState =
  | 'None'
  | 'Initial'
  | 'Downloading'
  | 'Completed'
  | 'Failed'
  | 'Cancelled'
  | 'Processed';

export interface Transfer {
  id: number;
  hash: string;
  name: string;
  category: string;
  size: number; // bytes
  putio_status: string; // raw put.io enum
  lifecycle_state: LifecycleState;
  percent_done: number; // 0.0–1.0 combined
  left_until_done: number; // bytes
  putio_phase: PutioPhase;
  local_phase: LocalPhase | null; // null when no ctx yet
  error: boolean;
  error_string: string;
  permanent: boolean;
  processed_at: string | null;
  created_at: string | null;
  finished_at: string | null;
}

export interface Disk {
  size: number; // bytes
  used: number; // bytes
  avail: number; // bytes
}

export interface Account {
  username: string;
  disk: Disk;
  used_percent: number; // 0.0–100.0
  over_quota: boolean;
  days_until_files_deletion: number;
}

export type SettingSource = 'env' | 'file' | 'flag' | 'default';

export interface SettingEntry {
  key: string;
  value: string | number | null;
  source: SettingSource;
  locked: boolean;
  live: boolean;
  restart_required: boolean;
  is_set?: boolean; // present only for the token entry
}

export interface LogEvent {
  time: string; // RFC3339
  level: string; // debug|info|warn|error|fatal
  component: string;
  message: string;
  fields: Record<string, unknown>;
}

export interface Snapshot {
  transfers: Transfer[];
  account: Account;
  settings: SettingEntry[];
  logs: LogEvent[];
}

export interface ApiError {
  error: {
    code: string;
    message: string;
    field?: string;
  };
}

// PUT /api/settings response.
export interface SettingsPutResponse {
  applied: string[];
  persisted: string[];
  restart_required: string[];
  settings: SettingEntry[];
}
