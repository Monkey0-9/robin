document.addEventListener('DOMContentLoaded', () => {
    // 1. Initialize Components
    initClock();
    renderIndices();
    renderMetals();
    renderHeatmap();
    renderPortfolio();
    initChart();
    
    // 2. Setup Drag and Drop
    setupDragAndDrop();
    
    // 3. Update time every second
    setInterval(updateTime, 1000);
});

function updateTime() {
    const now = new Date();
    document.getElementById('current-time').textContent = now.toISOString().replace('T', ' ').substring(0, 19) + ' UTC';
}

function initClock() {
    updateTime();
    const clocksContainer = document.getElementById('clocks-content');
    const cities = [
        { name: 'New York', tz: 'America/New_York' },
        { name: 'London', tz: 'Europe/London' },
        { name: 'Tokyo', tz: 'Asia/Tokyo' },
        { name: 'Sydney', tz: 'Australia/Sydney' }
    ];

    cities.forEach(city => {
        const div = document.createElement('div');
        div.className = 'clock-item';
        div.innerHTML = `
            <div>${city.name}</div>
            <div class="clock-time" id="clock-${city.name.replace(' ', '')}">--:--:--</div>
        `;
        clocksContainer.appendChild(div);
    });

    setInterval(() => {
        cities.forEach(city => {
            const timeStr = new Date().toLocaleTimeString('en-US', { timeZone: city.tz, hour12: false });
            document.getElementById(`clock-${city.name.replace(' ', '')}`).textContent = timeStr;
        });
    }, 1000);
}

function renderIndices() {
    const container = document.getElementById('indices-content');
    const data = DataAdapters.getIndicesData();
    
    container.innerHTML = data.map(item => {
        const colorClass = item.change >= 0 ? 'text-up' : 'text-down';
        const sign = item.change >= 0 ? '+' : '';
        return `
            <div class="ticker-row">
                <span>${item.symbol}</span>
                <span>${item.value.toFixed(2)}</span>
                <span class="${colorClass}">${sign}${item.change.toFixed(2)} (${sign}${item.pct.toFixed(2)}%)</span>
            </div>
        `;
    }).join('');
}

function renderMetals() {
    const container = document.getElementById('metals-content');
    const data = DataAdapters.getMetalsData();
    
    container.innerHTML = data.map(item => {
        const colorClass = item.change >= 0 ? 'text-up' : 'text-down';
        const sign = item.change >= 0 ? '+' : '';
        return `
            <div class="ticker-row">
                <span>${item.symbol}</span>
                <span>$${item.value.toFixed(2)}</span>
                <span class="${colorClass}">${sign}${item.pct.toFixed(2)}%)</span>
            </div>
        `;
    }).join('');
}

function renderPortfolio() {
    const container = document.getElementById('portfolio-content');
    const data = DataAdapters.getPortfolioData();
    
    container.innerHTML = data.map(item => {
        return `
            <div class="portfolio-item">
                <span>${item.symbol} (${item.shares} sh)</span>
                <span>$${item.value.toLocaleString()}</span>
                <span>${item.return}</span>
            </div>
        `;
    }).join('');
}

function renderHeatmap() {
    const container = document.getElementById('heatmap-content');
    const data = DataAdapters.getHeatmapData();
    
    container.innerHTML = data.map(item => {
        // Simple color scaling
        let bg = '#333';
        if (item.perf > 1) bg = 'rgba(0, 255, 136, 0.4)';
        else if (item.perf > 0) bg = 'rgba(0, 255, 136, 0.2)';
        else if (item.perf < -1) bg = 'rgba(255, 51, 102, 0.4)';
        else if (item.perf < 0) bg = 'rgba(255, 51, 102, 0.2)';

        return `
            <div class="heatmap-cell" style="background-color: ${bg}">
                <div>${item.symbol}</div>
                <div>${item.perf > 0 ? '+' : ''}${item.perf}%</div>
            </div>
        `;
    }).join('');
}

function initChart() {
    const ctx = document.getElementById('aaplCanvas').getContext('2d');
    const chartData = DataAdapters.getAAPLChartData();
    
    // Use Chart.js defaults to match dark theme
    Chart.defaults.color = '#888';
    Chart.defaults.font.family = 'Consolas, monospace';

    new Chart(ctx, {
        type: 'line',
        data: {
            labels: chartData.labels,
            datasets: [{
                label: 'AAPL',
                data: chartData.data,
                borderColor: '#00ffff',
                backgroundColor: 'rgba(0, 255, 255, 0.1)',
                borderWidth: 2,
                pointRadius: 0,
                fill: true,
                tension: 0.1
            }]
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            plugins: {
                legend: { display: false }
            },
            scales: {
                x: { grid: { color: '#333' } },
                y: { grid: { color: '#333' } }
            }
        }
    });
}

// --- Drag and Drop Logic with localStorage ---
function setupDragAndDrop() {
    const grid = document.getElementById('dashboard-grid');
    let draggedItem = null;

    // Load saved order
    const savedOrder = JSON.parse(localStorage.getItem('dashboardOrder'));
    if (savedOrder && savedOrder.length > 0) {
        const items = Array.from(grid.children);
        savedOrder.forEach(id => {
            const item = items.find(el => el.getAttribute('data-id') === id);
            if (item) grid.appendChild(item); // Reorder by appending
        });
    }

    const widgets = document.querySelectorAll('.widget');
    
    widgets.forEach(widget => {
        const handle = widget.querySelector('.drag-handle');
        
        handle.addEventListener('mousedown', () => {
            widget.setAttribute('draggable', 'true');
        });

        widget.addEventListener('dragstart', function(e) {
            draggedItem = this;
            setTimeout(() => this.classList.add('dragging'), 0);
        });

        widget.addEventListener('dragend', function(e) {
            this.classList.remove('dragging');
            widget.removeAttribute('draggable');
            saveOrder();
        });

        widget.addEventListener('dragover', function(e) {
            e.preventDefault();
            this.classList.add('drag-over');
        });

        widget.addEventListener('dragleave', function(e) {
            this.classList.remove('drag-over');
        });

        widget.addEventListener('drop', function(e) {
            this.classList.remove('drag-over');
            if (this !== draggedItem) {
                // Swap logic
                const allItems = [...grid.querySelectorAll('.widget')];
                const draggedIndex = allItems.indexOf(draggedItem);
                const droppedIndex = allItems.indexOf(this);

                if (draggedIndex < droppedIndex) {
                    this.after(draggedItem);
                } else {
                    this.before(draggedItem);
                }
            }
        });
    });
}

function saveOrder() {
    const grid = document.getElementById('dashboard-grid');
    const order = Array.from(grid.children).map(el => el.getAttribute('data-id'));
    localStorage.setItem('dashboardOrder', JSON.stringify(order));
}
