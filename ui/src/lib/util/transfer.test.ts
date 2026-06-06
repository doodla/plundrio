import { describe, it, expect } from 'vitest';
import type { LocalPhase, PutioPhase, Transfer } from '../types';
import {
  ARC_HALF,
  aggregate,
  combinedRate,
  displayState,
  focalTransfer,
  gaugeArcs,
  humanLabel,
  isActive,
  localFraction,
} from './transfer';

function putio(p: Partial<PutioPhase> = {}): PutioPhase {
  return { percent_done: 0, downloaded: 0, rate_download: 0, eta_seconds: 0, ...p };
}
function local(p: Partial<LocalPhase> = {}): LocalPhase {
  return {
    downloaded: 0,
    total: 0,
    completed_files: 0,
    failed_files: 0,
    total_files: 0,
    rate_download: 0,
    eta_seconds: 0,
    ...p,
  };
}
function tx(p: Partial<Transfer> = {}): Transfer {
  return {
    id: 1,
    hash: 'h',
    name: 'n',
    category: '',
    size: 1000,
    putio_status: 'IN_QUEUE',
    lifecycle_state: 'None',
    percent_done: 0,
    left_until_done: 1000,
    putio_phase: putio(),
    local_phase: null,
    error: false,
    error_string: '',
    permanent: false,
    processed_at: null,
    created_at: null,
    finished_at: null,
    ...p,
  };
}

describe('displayState', () => {
  it('queued for Initial/None at 0%', () => {
    expect(displayState(tx({ lifecycle_state: 'None', putio_status: 'IN_QUEUE' }))).toBe('queued');
    expect(displayState(tx({ lifecycle_state: 'Initial' }))).toBe('queued');
  });

  it('put.io-fetching when no ctx and fetch in progress', () => {
    const t = tx({
      lifecycle_state: 'None',
      putio_status: 'DOWNLOADING',
      putio_phase: putio({ percent_done: 60, rate_download: 5_000_000 }),
      percent_done: 0.3,
    });
    expect(displayState(t)).toBe('putio-fetching');
  });

  it('local-downloading when Downloading with a local phase mid-progress', () => {
    const t = tx({
      lifecycle_state: 'Downloading',
      putio_status: 'COMPLETED',
      putio_phase: putio({ percent_done: 100 }),
      local_phase: local({ downloaded: 760, total: 1000, rate_download: 8_000_000 }),
      percent_done: 0.88,
    });
    expect(displayState(t)).toBe('local-downloading');
  });

  it('completed for Processed at 100%', () => {
    const t = tx({
      lifecycle_state: 'Processed',
      putio_status: 'COMPLETED',
      putio_phase: putio({ percent_done: 100 }),
      local_phase: local({ downloaded: 1000, total: 1000, completed_files: 1, total_files: 1 }),
      percent_done: 1,
    });
    expect(displayState(t)).toBe('completed');
  });

  it('completed without ctx when put.io COMPLETED/SEEDING', () => {
    expect(
      displayState(
        tx({
          putio_status: 'SEEDING',
          putio_phase: putio({ percent_done: 100 }),
          percent_done: 0.5,
        }),
      ),
    ).toBe('completed');
  });

  it('failed whenever error is set (permanent cascade OR put.io ERROR)', () => {
    const perm = tx({ error: true, permanent: true, error_string: 'no space', percent_done: 0.32 });
    expect(displayState(perm)).toBe('failed');
    // put.io-side ERROR: error set, no ctx, permanent false — still failed/red.
    const putioErr = tx({
      lifecycle_state: 'None',
      putio_status: 'ERROR',
      error: true,
      permanent: false,
      error_string: 'no seeders',
    });
    expect(displayState(putioErr)).toBe('failed');
  });

  it('transient Failed (error:false) renders as its underlying phase, never red', () => {
    const transient = tx({
      lifecycle_state: 'Downloading',
      error: false,
      permanent: false,
      local_phase: local({ downloaded: 100, total: 1000 }),
      putio_phase: putio({ percent_done: 100 }),
      percent_done: 0.55,
    });
    expect(displayState(transient)).toBe('local-downloading');
  });
});

describe('humanLabel', () => {
  it('maps each state', () => {
    expect(humanLabel('queued')).toBe('queued');
    expect(humanLabel('putio-fetching')).toBe('fetching at put.io');
    expect(humanLabel('local-downloading')).toBe('local download');
    expect(humanLabel('completed')).toBe('completed');
    expect(humanLabel('failed')).toBe('failed');
  });
});

describe('isActive (stall semantics)', () => {
  it('false when active-phase rate is 0', () => {
    const t = tx({
      lifecycle_state: 'Downloading',
      local_phase: local({ downloaded: 500, total: 1000, rate_download: 0 }),
      putio_phase: putio({ percent_done: 100 }),
      percent_done: 0.75,
    });
    expect(isActive(t, 'local-downloading')).toBe(false);
  });
  it('true when rate > 0', () => {
    const t = tx({ putio_phase: putio({ percent_done: 50, rate_download: 9 }) });
    expect(isActive(t, 'putio-fetching')).toBe(true);
  });
});

describe('localFraction', () => {
  it('uses downloaded/total when present', () => {
    expect(localFraction(tx({ local_phase: local({ downloaded: 760, total: 1000 }) }))).toBeCloseTo(
      0.76,
    );
  });
  it('falls back to percent_done identity (pct*2-1)', () => {
    expect(localFraction(tx({ percent_done: 0.75 }))).toBeCloseTo(0.5);
    expect(localFraction(tx({ percent_done: 0.4 }))).toBe(0); // clamped
  });
});

describe('gaugeArcs', () => {
  it('full put.io leg + half local leg at 75% combined', () => {
    const t = tx({
      putio_phase: putio({ percent_done: 100 }),
      local_phase: local({ downloaded: 500, total: 1000 }),
      percent_done: 0.75,
    });
    const a = gaugeArcs(t);
    expect(a.p1Len).toBeCloseTo(ARC_HALF); // 197.9
    expect(a.p2Len).toBeCloseTo(ARC_HALF * 0.5); // 98.95
  });
});

describe('combinedRate', () => {
  it('sums both phases', () => {
    expect(
      combinedRate(
        tx({ putio_phase: putio({ rate_download: 5 }), local_phase: local({ rate_download: 23 }) }),
      ),
    ).toBe(28);
  });
});

describe('aggregate', () => {
  it('counts active/queued/stalled and sums rates', () => {
    const list = [
      tx({ id: 1, lifecycle_state: 'Initial' }), // queued + active
      tx({
        id: 2,
        lifecycle_state: 'Downloading',
        putio_phase: putio({ percent_done: 60, rate_download: 5 }),
      }), // putio-fetching active
      tx({
        id: 3,
        lifecycle_state: 'Downloading',
        local_phase: local({ downloaded: 1, total: 10, rate_download: 0 }),
        putio_phase: putio({ percent_done: 100 }),
        percent_done: 0.55,
      }), // local-downloading STALLED
    ];
    const a = aggregate(list);
    expect(a.activeCount).toBe(3);
    expect(a.queuedCount).toBe(1);
    expect(a.stalledCount).toBe(1);
    expect(a.putioRate).toBe(5);
  });
});

describe('focalTransfer', () => {
  it('prefers local-downloading over fetching', () => {
    const fetching = tx({
      id: 1,
      lifecycle_state: 'Downloading',
      putio_phase: putio({ percent_done: 50, rate_download: 1 }),
      percent_done: 0.25,
    });
    const localDl = tx({
      id: 2,
      lifecycle_state: 'Downloading',
      local_phase: local({ downloaded: 5, total: 10, rate_download: 1 }),
      putio_phase: putio({ percent_done: 100 }),
      percent_done: 0.75,
    });
    expect(focalTransfer([fetching, localDl])?.id).toBe(2);
  });
  it('null on empty list', () => {
    expect(focalTransfer([])).toBeNull();
  });
});
