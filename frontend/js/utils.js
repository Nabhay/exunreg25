function isMobile() {
    return window.innerHeight > window.innerWidth || window.innerWidth <= 768;
}
function isMobile() {
    return window.innerHeight > window.innerWidth || window.innerWidth <= 768;
}

async function loadComponent(componentPath, targetElement) {
    try {
        const response = await fetch(componentPath);
        const html = await response.text();
        if (targetElement) {
            if (targetElement === document.body) {
                document.body.insertAdjacentHTML('beforeend', html);
            } else {
                targetElement.innerHTML = html;
            }
        }
        return html;
    } catch (error) {
        console.error('Error loading component:', error);
        return '';
    }
}
    window.Utils = {
        isMobile,
        loadComponent,
        formatDate,
        showToast,
        debounce,
        validateEmail,
        validatePhone,
        sanitizeHTML,
        copyToClipboard,
        generateRandomId,
        formatFileSize,
        throttle,
        handleFormSubmit,
        setLoading,
        redirect,
        formatEventMode,
        formatParticipants,
        formatEligibility
    };

function formatDate(date) {
    return new Date(date).toLocaleDateString('en-US', {
        year: 'numeric',
        month: 'long',
        day: 'numeric'
    });
}

function showToast(message, type = 'info') {
    const toast = document.createElement('div');
    toast.className = `toast toast--${type}`;
    toast.textContent = message;
    
    document.body.appendChild(toast);
    
    setTimeout(() => {
        toast.classList.add('toast--show');
    }, 100);

    setTimeout(() => {
        toast.classList.remove('toast--show');
        setTimeout(() => {
            document.body.removeChild(toast);
        }, 300);
    }, 3000);
}

function debounce(func, wait) {
    let timeout;
    return function executedFunction(...args) {
        const later = () => {
            clearTimeout(timeout);
            func(...args);
        };
        clearTimeout(timeout);
        timeout = setTimeout(later, wait);
    };
}

function validateEmail(email) {
    const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
    return emailRegex.test(email);
}

function validatePhone(phone) {
    const phoneRegex = /^\+?[\d\s\-\(\)]{10,}$/;
    return phoneRegex.test(phone);
}

function sanitizeHTML(str) {
    const temp = document.createElement('div');
    temp.textContent = str;
    return temp.innerHTML;
}

async function copyToClipboard(text) {
    try {
        await navigator.clipboard.writeText(text);
        showToast('Copied to clipboard!', 'success');
    } catch (error) {
        console.error('Failed to copy:', error);
        showToast('Failed to copy to clipboard', 'error');
    }
}

function generateRandomId() {
    return Math.random().toString(36).substr(2, 9);
}

function formatFileSize(bytes) {
    if (bytes === 0) return '0 Bytes';
    const k = 1024;
    const sizes = ['Bytes', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

function throttle(func, limit) {
    let inThrottle;
    return function() {
        const args = arguments;
        const context = this;
        if (!inThrottle) {
            func.apply(context, args);
            inThrottle = true;
            setTimeout(() => inThrottle = false, limit);
        }
    };
}

async function handleFormSubmit(form, submitHandler) {
    const formData = new FormData(form);
    const data = Object.fromEntries(formData.entries());
    
    try {
        await submitHandler(data);
    } catch (error) {
        console.error('Form submission error:', error);
        showToast(error.message || 'An error occurred', 'error');
    }
}

function setLoading(element, isLoading) {
    if (isLoading) {
        element.disabled = true;
        element.classList.add('loading');
        element.dataset.originalText = element.textContent;
        element.textContent = 'Loading...';
    } else {
        element.disabled = false;
        element.classList.remove('loading');
        element.textContent = element.dataset.originalText || element.textContent;
    }
}

function redirect(url, delay = 0) {
    setTimeout(() => {
        window.location.href = url;
    }, delay);
}

function formatEventMode(mode) {
    if (!mode || typeof mode !== 'string') return '';
    return mode.charAt(0).toUpperCase() + mode.slice(1).toLowerCase();
}

function formatParticipants(count) {
    if (count === 1) return '1 participant';
    return `${count} participants`;
}

function formatEligibility(eligibility, openToAll) {
    if (openToAll) return 'Open to All'
    const nums = (String(eligibility).match(/\d+/g) || []).map(n => n.trim())
    if (nums.length >= 2) return `${nums[0]}th - ${nums[nums.length - 1]}th`
    if (nums.length === 1) return `${nums[0]}th`
    const clean = String(eligibility).trim()
    return clean === '' ? 'All Classes' : clean
}

function escapeHtml(str) {
    return sanitizeHTML(String(str || ''));
}

window.Utils.escapeHtml = escapeHtml;

function showConfirmModal(message, title = 'Confirm', confirmText = 'Confirm', cancelText = 'Cancel') {
    return new Promise((resolve) => {
        const overlay = document.createElement('div');
        overlay.style.position = 'fixed';
        overlay.style.inset = '0';
        overlay.style.background = 'rgba(0,0,0,0.35)';
        overlay.style.display = 'flex';
        overlay.style.alignItems = 'center';
        overlay.style.justifyContent = 'center';
        overlay.style.zIndex = 9999;
        overlay.tabIndex = 0;

        const box = document.createElement('div');
        box.style.background = '#fff';
        box.style.borderRadius = '10px';
        box.style.padding = '20px';
        box.style.maxWidth = '480px';
        box.style.width = '92%';
        box.style.boxShadow = '0 20px 60px rgba(2,6,23,0.2)';
        box.style.fontFamily = "'Raleway', sans-serif";

        const titleEl = document.createElement('div');
        titleEl.style.fontSize = '16px';
        titleEl.style.fontWeight = '700';
        titleEl.style.marginBottom = '8px';
        titleEl.textContent = title || 'Confirm';

        const desc = document.createElement('div');
        desc.style.fontSize = '14px';
        desc.style.color = '#374151';
        desc.style.marginBottom = '16px';
        desc.textContent = message || '';

        const actions = document.createElement('div');
        actions.style.display = 'flex';
        actions.style.justifyContent = 'flex-end';
        actions.style.gap = '10px';

        const cancelBtn = document.createElement('button');
        cancelBtn.className = 'btn btn--secondary';
        cancelBtn.textContent = cancelText || 'Cancel';
        const confirmBtn = document.createElement('button');
        confirmBtn.className = 'btn btn--primary';
        confirmBtn.textContent = confirmText || 'Confirm';

        actions.appendChild(cancelBtn);
        actions.appendChild(confirmBtn);

        box.appendChild(titleEl);
        box.appendChild(desc);
        box.appendChild(actions);
        overlay.appendChild(box);
        document.body.appendChild(overlay);

        const cleanup = () => { overlay.removeEventListener('keydown', onKeyDown); overlay.remove(); };
        const onKeyDown = (ev) => { if (ev.key === 'Escape') { cleanup(); resolve(false); } if (ev.key === 'Enter') { cleanup(); resolve(true); } };
        cancelBtn.addEventListener('click', () => { cleanup(); resolve(false); });
        confirmBtn.addEventListener('click', () => { cleanup(); resolve(true); });
        overlay.addEventListener('click', (ev) => { if (ev.target === overlay) { cleanup(); resolve(false); } });
        overlay.addEventListener('keydown', onKeyDown);
        setTimeout(() => { overlay.focus(); cancelBtn.focus(); }, 10);
    });
}

if (window && window.Utils) {
    window.Utils.showConfirmModal = showConfirmModal;
}
