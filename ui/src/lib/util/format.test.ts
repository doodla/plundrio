import { describe, it, expect } from 'vitest';
import { humanBytes, humanEta, humanRate, formatFields } from './format';

describe('humanBytes', () => {
  it('formats SI magnitudes', () => {
    expect(humanBytes(0)).toBe('0 B');
    expect(humanBytes(512)).toBe('512 B');
    expect(humanBytes(12_000_000)).toBe('12.0 MB');
    expect(humanBytes(1_000_000_000_000)).toBe('1.0 TB');
    expect(humanBytes(620_000_000_000)).toBe('620.0 GB');
  });
});

describe('humanRate', () => {
  it('dashes a zero rate', () => {
    expect(humanRate(0)).toBe('—');
    expect(humanRate(8_400_000)).toBe('8.4 MB/s');
  });
});

describe('humanEta', () => {
  it('formats m:ss and h:mm:ss, dashing unknown', () => {
    expect(humanEta(0)).toBe('—');
    expect(humanEta(54)).toBe('0:54');
    expect(humanEta(120)).toBe('2:00');
    expect(humanEta(3661)).toBe('1:01:01');
  });
});

describe('formatFields', () => {
  it('renders k=v and skips null', () => {
    expect(formatFields({ id: 8821, name: 'x', skip: null })).toBe('id=8821 name=x');
  });
});
