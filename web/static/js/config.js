let editor = ace.edit("editor");
editor.setTheme("ace/theme/monokai");
editor.session.setMode("ace/mode/json");
editor.setOptions({
    fontSize: "14px"
});

function showMessage(msg, isError = false) {
    const msgDiv = document.getElementById('message');
    msgDiv.textContent = msg;
    msgDiv.className = isError ? 'error' : 'success';
    msgDiv.style.display = 'block';
    setTimeout(() => {
        msgDiv.style.display = 'none';
    }, 5000);
}

async function loadConfig() {
    try {
        const response = await fetch('/metrics/config/get');
        if (!response.ok) {
            throw new Error('加载配置失败');
        }
        const config = await response.json();
        editor.setValue(JSON.stringify(config, null, 2), -1);
        showMessage('配置已加载');
    } catch (error) {
        showMessage(error.message, true);
    }
}

async function saveConfig() {
    try {
        const config = JSON.parse(editor.getValue());
        const response = await fetch('/metrics/config/save', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify(config)
        });

        if (!response.ok) {
            const error = await response.text();
            throw new Error(error);
        }

        const result = await response.json();
        showMessage(result.message);
    } catch (error) {
        showMessage(error.message, true);
    }
}

function formatJson() {
    try {
        const config = JSON.parse(editor.getValue());
        editor.setValue(JSON.stringify(config, null, 2), -1);
        showMessage('JSON已格式化');
    } catch (error) {
        showMessage('JSON格式错误: ' + error.message, true);
    }
}

// 初始加载配置
loadConfig(); 