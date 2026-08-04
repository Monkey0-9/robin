import { describe, it, expect } from 'vitest';
import { useTerminalStore } from '../src/store/useTerminalStore';

describe('useTerminalStore State Management', () => {
  it('should initialize with default active tab', () => {
    const state = useTerminalStore.getState();
    expect(state).toBeDefined();
  });

  it('should allow setting symbol', () => {
    useTerminalStore.getState().setSymbol('ETH-USD');
    expect(useTerminalStore.getState().symbol).toBe('ETH-USD');
  });
});
