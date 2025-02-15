async function loadMetrics() {
    try {
        const token = localStorage.getItem('token');
        if (!token) {
            window.location.href = '/metrics/ui';
            return;
        }

        const response = await fetch('/metrics', {
            headers: {
                'Authorization': `Bearer ${token}`
            }
        });

        if (!response.ok) {
            if (response.status === 401) {
                window.location.href = '/metrics/ui';
                return;
            }
            throw new Error('加载监控数据失败');
        }

        const metrics = await response.json();
        displayMetrics(metrics);
    } catch (error) {
        showMessage(error.message, true);
    }
}

function displayMetrics(metrics) {
    const container = document.getElementById('metrics');
    container.innerHTML = '';

    // 添加基本信息
    addSection(container, '基本信息', {
        '运行时间': metrics.uptime,
        '总请求数': metrics.totalRequests,
        '活跃请求数': metrics.activeRequests,
        '错误请求数': metrics.totalErrors,
        '总传输字节': formatBytes(metrics.totalBytes)
    });

    // 添加状态码统计
    addSection(container, '状态码统计', metrics.statusStats);

    // 添加路径统计
    addSection(container, '路径统计', metrics.pathStats);

    // 添加来源统计
    addSection(container, '来源统计', metrics.refererStats);

    // 添加延迟统计
    addSection(container, '延迟统计', {
        '平均延迟': `${metrics.avgLatency}ms`,
        '延迟分布': metrics.latencyBuckets
    });
}

function addSection(container, title, data) {
    const section = document.createElement('div');
    section.className = 'metrics-section';
    
    const titleElem = document.createElement('h2');
    titleElem.textContent = title;
    section.appendChild(titleElem);

    const content = document.createElement('div');
    content.className = 'metrics-content';

    for (const [key, value] of Object.entries(data)) {
        const item = document.createElement('div');
        item.className = 'metrics-item';
        item.innerHTML = `<span class="key">${key}:</span> <span class="value">${value}</span>`;
        content.appendChild(item);
    }

    section.appendChild(content);
    container.appendChild(section);
}

function formatBytes(bytes) {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

function showMessage(msg, isError = false) {
    const msgDiv = document.getElementById('message');
    if (!msgDiv) return;
    
    msgDiv.textContent = msg;
    msgDiv.className = isError ? 'error' : 'success';
    msgDiv.style.display = 'block';
    setTimeout(() => {
        msgDiv.style.display = 'none';
    }, 5000);
}

// 初始加载监控数据
loadMetrics();

// 每30秒刷新一次数据
setInterval(loadMetrics, 30000); 