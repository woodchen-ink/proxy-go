async function login() {
    const password = document.getElementById('password').value;
    
    try {
        const response = await fetch('/metrics/auth', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ password })
        });

        if (!response.ok) {
            throw new Error('登录失败');
        }

        const data = await response.json();
        localStorage.setItem('token', data.token);
        window.location.href = '/metrics/dashboard';
    } catch (error) {
        showMessage(error.message, true);
    }
}

function showMessage(msg, isError = false) {
    const msgDiv = document.getElementById('message');
    msgDiv.textContent = msg;
    msgDiv.className = isError ? 'error' : 'success';
    msgDiv.style.display = 'block';
    setTimeout(() => {
        msgDiv.style.display = 'none';
    }, 5000);
}

// 添加回车键监听
document.getElementById('password').addEventListener('keypress', function(e) {
    if (e.key === 'Enter') {
        login();
    }
}); 