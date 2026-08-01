# Global Market Monitor Terminal

A completely offline, dependency-free (except local libraries), terminal-style dashboard for market monitoring.

## Features
- **Offline Ready**: No server required. Just open `index.html` in your browser. Chart.js is bundled locally.
- **Drag and Drop**: Reorder widgets using the `≡` handle. Order is saved locally in your browser.
- **Widgets**:
  - Global Indices Ticker
  - AI / Energy / Financials Heatmap
  - AAPL 60-Session Chart (Canvas)
  - Precious Metals Ticker
  - World Session Clocks
  - Portfolio Holdings (styled with requested colors)

## How to use
1. Unzip this folder anywhere on your computer.
2. Double click `index.html` to open it in Chrome, Firefox, or Safari.
3. No build tools (npm/yarn) are required.

## Customization
- **Theme**: Edit `css/style.css`.
- **Mock Data**: Edit `js/data-adapters.js` to change the simulated values or hook them up to real APIs (e.g., AlphaVantage, Finnhub).
