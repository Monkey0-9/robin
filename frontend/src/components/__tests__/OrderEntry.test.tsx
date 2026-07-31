import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import OrderEntry from '../OrderEntry';

const mockSubmitOrder = vi.fn();
const mockSetRoutingMode = vi.fn();

vi.mock('../../store/useTerminalStore', () => ({
  useTerminalStore: () => ({
    selectedSymbol: 'BTC/USD',
    assets: [{ symbol: 'BTC/USD', currentPrice: 64000 }],
    submitOrder: mockSubmitOrder,
    balance: 10000,
    routingMode: 'AUTO',
    setRoutingMode: mockSetRoutingMode,
  }),
}));

describe('OrderEntry', () => {
  beforeEach(() => {
    mockSubmitOrder.mockClear();
    mockSetRoutingMode.mockClear();
  });

  it('submits a market BUY order with current price when submit is clicked', () => {
    render(<OrderEntry />);

    fireEvent.click(screen.getByText('SUBMIT BUY MARKET ORDER'));

    expect(mockSubmitOrder).toHaveBeenCalledWith(
      'BTC/USD',
      'BUY',
      64000,
      1.0,
      true,
      'MARKET'
    );
  });

  it('submits a SELL order when SELL is selected', () => {
    render(<OrderEntry />);

    fireEvent.click(screen.getByText('SELL'));
    fireEvent.click(screen.getByText('SUBMIT SELL MARKET ORDER'));

    expect(mockSubmitOrder).toHaveBeenCalledWith(
      'BTC/USD',
      'SELL',
      64000,
      1.0,
      true,
      'MARKET'
    );
  });

  it('submits a LIMIT order at the custom price when limit type is chosen', () => {
    render(<OrderEntry />);

    fireEvent.click(screen.getByText('Limit'));
    const priceInput = screen.getByDisplayValue('64000.00');
    fireEvent.change(priceInput, { target: { value: '65000' } });
    fireEvent.change(screen.getByDisplayValue('1.0'), { target: { value: '0.1' } });
    fireEvent.click(screen.getByText('SUBMIT BUY LIMIT ORDER'));

    expect(mockSubmitOrder).toHaveBeenCalledWith(
      'BTC/USD',
      'BUY',
      65000,
      0.1,
      false,
      'LIMIT'
    );
  });

  it('switches routing mode via the select', () => {
    render(<OrderEntry />);

    fireEvent.change(screen.getByDisplayValue('Best Price (Auto-Route)'), {
      target: { value: 'NYSE' },
    });

    expect(mockSetRoutingMode).toHaveBeenCalledWith('NYSE');
  });

  it('shows the available balance', () => {
    render(<OrderEntry />);
    expect(screen.getByText('$10000.00')).toBeInTheDocument();
  });
});
