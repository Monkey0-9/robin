// Simulated Data Adapters for Terminal Dashboard

const DataAdapters = {
    // Generate 60 periods of random walk data for AAPL
    getAAPLChartData: () => {
        let price = 175.00;
        const labels = [];
        const data = [];
        const now = new Date();
        
        for(let i = 60; i >= 0; i--) {
            const d = new Date(now.getTime() - (i * 24 * 60 * 60 * 1000));
            labels.push(`${d.getMonth()+1}/${d.getDate()}`);
            
            // Random walk
            const change = (Math.random() - 0.48) * 3;
            price = price + change;
            data.push(price.toFixed(2));
        }
        return { labels, data };
    },

    getIndicesData: () => {
        return [
            { symbol: 'S&P 500', value: 5123.45, change: 12.30, pct: 0.24 },
            { symbol: 'NASDAQ', value: 16234.12, change: -45.20, pct: -0.28 },
            { symbol: 'DOW JONES', value: 39123.00, change: 105.10, pct: 0.27 },
            { symbol: 'NIKKEI', value: 39500.20, change: 210.50, pct: 0.54 },
            { symbol: 'FTSE 100', value: 7654.30, change: -12.40, pct: -0.16 }
        ];
    },

    getMetalsData: () => {
        return [
            { symbol: 'GOLD (XAU)', value: 2154.30, change: 5.20, pct: 0.24 },
            { symbol: 'SILVER (XAG)', value: 24.50, change: -0.15, pct: -0.61 },
            { symbol: 'PLATINUM (XPT)', value: 915.20, change: 12.00, pct: 1.33 },
            { symbol: 'COPPER (HG)', value: 3.95, change: 0.02, pct: 0.51 }
        ];
    },

    getHeatmapData: () => {
        return [
            { sector: 'AI / Tech', symbol: 'NVDA', perf: 2.4 },
            { sector: 'AI / Tech', symbol: 'MSFT', perf: 1.1 },
            { sector: 'AI / Tech', symbol: 'AMD', perf: -0.5 },
            { sector: 'Energy', symbol: 'XOM', perf: -1.2 },
            { sector: 'Energy', symbol: 'CVX', perf: -0.8 },
            { sector: 'Energy', symbol: 'BP', perf: 0.3 },
            { sector: 'Financials', symbol: 'JPM', perf: 0.9 },
            { sector: 'Financials', symbol: 'GS', perf: 1.5 },
            { sector: 'Financials', symbol: 'BAC', perf: -0.2 }
        ];
    },

    getPortfolioData: () => {
        return [
            { symbol: 'AAPL', shares: 100, value: 17500, return: '+5.2%' },
            { symbol: 'TSLA', shares: 50, value: 10000, return: '-2.1%' },
            { symbol: 'BTC', shares: 0.5, value: 32000, return: '+12.4%' }
        ];
    }
};
