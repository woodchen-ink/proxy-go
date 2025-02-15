// 检查认证状态
function checkAuth() {
    const token = localStorage.getItem('token');
    if (!token) {
        window.location.href = '/admin/login';
        return false;
    }
    return true;
}

// 登录函数
async function login() {
    const password = document.getElementById('password').value;
    
    try {
        const response = await fetch('/admin/auth', {
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
        window.location.href = '/admin/metrics';
    } catch (error) {
        showToast(error.message, true);
    }
}

// 退出登录
function logout() {
    localStorage.removeItem('token');
    window.location.href = '/admin/login';
}

// 获取认证头
function getAuthHeaders() {
    const token = localStorage.getItem('token');
    return {
        'Authorization': `Bearer ${token}`,
        'Content-Type': 'application/json'
    };
}

// 显示提示消息
function showToast(message, isError = false) {
    const toast = document.createElement('div');
    toast.className = `toast toast-end ${isError ? 'alert alert-error' : 'alert alert-success'}`;
    toast.innerHTML = `<span>${message}</span>`;
    document.body.appendChild(toast);
    
    setTimeout(() => {
        toast.remove();
    }, 3000);
} 