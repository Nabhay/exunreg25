class LoginPage {
constructor() {
        this.authMode = 'login';
        this.currentEmail = '';
        this.otpResendTimeout = null;
        this.init();
    }

    init() {
        this.setupEventListeners();
        this.updateUI();
    }

    setupEventListeners() {
        const authForm = document.getElementById('auth-form');
        const otpForm = document.getElementById('otp-form');
    const setPasswordForm = document.getElementById('set-password-form');
        const switchBtn = document.getElementById('auth-switch');
        
        if (authForm) {
            authForm.addEventListener('submit', (e) => this.handleAuthSubmit(e));
        }
        
        if (otpForm) {
            otpForm.addEventListener('submit', (e) => this.handleOTPSubmit(e));
        }

        if (setPasswordForm) {
            setPasswordForm.addEventListener('submit', (e) => this.handleSetPassword(e));
        }
        
        if (switchBtn) {
            switchBtn.addEventListener('click', (e) => {
                e.preventDefault();
                this.switchAuthMode();
            });
        }

        const forgotLink = document.getElementById('forgot-password');
        if (forgotLink) {
            forgotLink.addEventListener('click', (e) => {
                e.preventDefault();
                this.handleForgotPassword();
            });
        }

        this.setupOTPInputs();
        this.setupResendOTP();
    }

    setupOTPInputs() {
        const otpInputs = document.querySelectorAll('.otp-input');
        
        otpInputs.forEach((input, index) => {
            input.addEventListener('input', (e) => {
                const value = e.target.value;
                
                if (value.length === 1 && index < otpInputs.length - 1) {
                    otpInputs[index + 1].focus();
                }
                
                if (value.length === 0 && index > 0) {
                    otpInputs[index - 1].focus();
                }
                
                e.target.value = value.slice(0, 1);
            });
            
            input.addEventListener('keydown', (e) => {
                if (e.key === 'Backspace' && input.value === '' && index > 0) {
                    otpInputs[index - 1].focus();
                }
            });
            
            input.addEventListener('paste', (e) => {
                e.preventDefault();
                let pasteData = '';
                try {
                    pasteData = (e.clipboardData && e.clipboardData.getData) ? e.clipboardData.getData('text') : (window.clipboardData && window.clipboardData.getData) ? window.clipboardData.getData('text') : '';
                } catch (err) {
                    pasteData = '';
                }
                const digits = (pasteData.match(/\d/g) || []).slice(0, otpInputs.length);
                if (digits.length === 0) return;
                digits.forEach((char, i) => {
                    if (index + i < otpInputs.length) {
                        otpInputs[index + i].value = char;
                    }
                });
                const nextIndex = Math.min(index + digits.length, otpInputs.length - 1);
                otpInputs[nextIndex].focus();
            });
        });
    }

    setupResendOTP() {
        const resendLink = document.getElementById('resend-otp');
        if (resendLink) {
            resendLink.addEventListener('click', (e) => {
                e.preventDefault();
                this.resendOTP();
            });
        }
    }

    async handleAuthSubmit(e) {
        e.preventDefault();
        
        const formData = new FormData(e.target);
        const email = formData.get('email');
        const password = formData.get('password');
        
        if (!this.validateEmail(email)) {
            this.showFieldError('email', 'Please enter a valid email address');
            return;
        }

        const submitBtn = e.target.querySelector('button[type="submit"]');
        Utils.setLoading(submitBtn, true);
        
        try {
            if (this.authMode === 'login') {
                await this.handleLogin(email, password);
            } else {
                    await this.handleRegister(email);
            }
        } catch (error) {
            console.error('Auth error:', error);
            Utils.showToast(error.message || 'Authentication failed', 'error');
        } finally {
            Utils.setLoading(submitBtn, false);
        }
    }

    async handleLogin(email, password) {
        if (!password) {
            this.showFieldError('password', 'Password is required');
            return;
        }

        try {
            const response = await ExunServices.api.apiRequest('/users/login', {
                method: 'POST',
                body: JSON.stringify({ email, password })
            });
            
            if (response.status === 'success') {
                Utils.showToast('Login successful!', 'success');
                const loginToken = (response.data && response.data.token) || response.token || null;
                if (loginToken) ExunServices.api.setAuthToken(loginToken);
                setTimeout(() => window.location.href = '/summary', 1000);
            } else {
                throw new Error(response.error || 'Login failed');
            }
        } catch (error) {
            throw new Error(error.message || 'Login failed');
        }
    }

    async handleRegister(email, schoolCode) {
        this.currentEmail = email;
        try {
            const response = await ExunServices.auth.sendOTP(email);
            if (response.success) {
                this.showOTPForm();
                Utils.showToast(response.message || 'OTP sent to your email', 'success');
                this.startResendTimer();
            } else if (response.type === 'user_exists') {
                Utils.showToast(response.message || 'Account exists — please sign in', 'info');
                this.authMode = 'login';
                this.updateUI();
                const emailEl = document.getElementById('email');
                if (emailEl) emailEl.value = email;
                const pwdEl = document.getElementById('password');
                if (pwdEl) pwdEl.focus();
            } else {
                throw new Error(response.message || 'Failed to send OTP');
            }
        } catch (error) {
            throw new Error(error.message || 'Failed to send OTP');
        }
    }

    async handleOTPSubmit(e) {
        e.preventDefault();
        
        const otpInputs = document.querySelectorAll('.otp-input');
        const otp = Array.from(otpInputs).map(input => input.value).join('');
        
        if (otp.length !== 6) {
            Utils.showToast('Please enter all 6 digits', 'error');
            return;
        }

        const submitBtn = e.target.querySelector('button[type="submit"]');
        Utils.setLoading(submitBtn, true);
        
        try {
            const response = await ExunServices.api.apiRequest('/auth/verify-otp', {
                method: 'POST',
                body: JSON.stringify({ email: this.currentEmail, otp })
            });

            this.lastOTP = otp;
            
            if (response.status === 'success') {
                const otpToken = (response.data && response.data.token) || response.token || null;
                if (otpToken) {
                    ExunServices.api.setAuthToken(otpToken);
                }

                if (this.authMode === 'register' || response.needs_signup) {
                    Utils.showToast('OTP verified! Set a password to complete registration', 'success');
                    this.showSetPasswordForm();
                } else {
                    if (this.authMode === 'reset') {
                        Utils.showToast('OTP verified! Set a new password', 'success');
                        this.showSetPasswordForm();
                    } else {
                        Utils.showToast('Login successful!', 'success');
                        setTimeout(() => window.location.href = '/summary', 1000);
                    }
                }
            } else {
                throw new Error(response.error || 'Invalid OTP');
            }
        } catch (error) {
            console.error('OTP verification error:', error);
            Utils.showToast(error.message || 'OTP verification failed', 'error');
            this.clearOTPInputs();
        } finally {
            Utils.setLoading(submitBtn, false);
        }
    }

    showSetPasswordForm() {
        const otpContainer = document.getElementById('otp-container');
        const setPasswordContainer = document.getElementById('set-password-container');
        if (otpContainer) otpContainer.style.display = 'none';
        if (setPasswordContainer) setPasswordContainer.style.display = 'block';
    }

    async handleSetPassword(e) {
        e.preventDefault();
        const pwd = document.getElementById('new_password')?.value || '';
        const confirm = document.getElementById('confirm_password')?.value || '';
        if (pwd.length < 6) {
            Utils.showToast('Password must be at least 6 characters', 'error');
            return;
        }
        if (pwd !== confirm) {
            Utils.showToast('Passwords do not match', 'error');
            return;
        }
        try {
            if (this.authMode === 'reset') {
                const response = await ExunServices.api.apiRequest('/auth/reset-password', { method: 'POST', body: JSON.stringify({ email: this.currentEmail, otp: this.lastOTP, new_password: pwd }) });
                if (response.status === 'success') {
                    Utils.showToast('Password reset successful. Redirecting to login...', 'success');
                    setTimeout(() => window.location.href = '/login', 800);
                } else {
                    Utils.showToast(response.error || 'Failed to reset password', 'error');
                }
            } else {
                const response = await ExunServices.api.apiRequest('/auth/complete', { method: 'POST', body: JSON.stringify({ username: this.currentEmail, password: pwd }) });
                if (response.status === 'success') {
                    Utils.showToast('Signup complete. Redirecting to profile completion...', 'success');
                    setTimeout(() => window.location.href = '/complete', 800);
                } else {
                    Utils.showToast(response.error || 'Failed to set password', 'error');
                }
            }
        } catch (err) {
            Utils.showToast(err.message || 'Failed to set password', 'error');
        }
    }

    async resendOTP() {
        const resendLink = document.getElementById('resend-otp');
        Utils.setLoading(resendLink, true);
        
        try {
            let response;
            if (this.authMode === 'reset') {
                response = await ExunServices.auth.sendResetOTP(this.currentEmail);
            } else {
                response = await ExunServices.auth.sendOTP(this.currentEmail);
            }
            if (response.success) {
                Utils.showToast('New OTP sent to your email', 'success');
                this.startResendTimer();
                this.clearOTPInputs();
            } else if (response.type === 'user_exists') {
                Utils.showToast(response.message || 'Account exists — please sign in', 'info');
                this.authMode = 'login';
                this.updateUI();
                const emailEl = document.getElementById('email');
                if (emailEl) emailEl.value = this.currentEmail;
                const pwdEl = document.getElementById('password');
                if (pwdEl) pwdEl.focus();
            } else {
                throw new Error(response.message || 'Failed to resend OTP');
            }
        } catch (error) {
            console.error('Resend OTP error:', error);
            Utils.showToast(error.message || 'Failed to resend OTP', 'error');
        } finally {
            Utils.setLoading(resendLink, false);
        }
    }

    async handleForgotPassword() {
        const emailEl = document.getElementById('email');
        const email = (emailEl && emailEl.value) ? emailEl.value.trim() : '';
        if (!this.validateEmail(email)) {
            this.showFieldError('email', 'Please enter a valid email address');
            return;
        }
        const resp = await ExunServices.auth.sendResetOTP(email);
        if (resp.success) {
            this.currentEmail = email;
            this.authMode = 'reset';
            this.showOTPForm();
            Utils.showToast(resp.message || 'OTP sent to your email', 'success');
            this.startResendTimer();
        } else if (resp.type === 'user_not_found') {
            Utils.showToast(resp.message || 'No account found with this email', 'error');
        } else {
            Utils.showToast(resp.message || 'Failed to send OTP', 'error');
        }
    }

    switchAuthMode() {
        this.authMode = this.authMode === 'login' ? 'register' : 'login';
        this.updateUI();
        this.clearErrors();
    }

    updateUI() {
        const title = document.getElementById('auth-title');
        const subtitle = document.getElementById('auth-subtitle');
        const submitBtn = document.getElementById('auth-submit');
        const switchText = document.getElementById('switch-text');
        const switchLink = document.getElementById('auth-switch');
        const passwordGroup = document.getElementById('password-group');

        if (this.authMode === 'login') {
            if (title) title.textContent = 'Welcome Back';
            if (subtitle) subtitle.textContent = 'Sign in to your Exun 2025 account';
            if (submitBtn) submitBtn.textContent = 'Sign In';
            if (switchText) switchText.textContent = "Don't have an account? ";
            if (switchLink) switchLink.textContent = 'Register here';
            if (passwordGroup) passwordGroup.style.display = 'flex';
        } else {
            if (title) title.textContent = 'Join Exun 2025';
            if (subtitle) subtitle.textContent = 'Create your account to register for events';
            if (submitBtn) submitBtn.textContent = 'Send OTP';
            if (switchText) switchText.textContent = 'Already have an account? ';
            if (switchLink) switchLink.textContent = 'Sign in here';
            if (passwordGroup) passwordGroup.style.display = 'none';
        }
    }

    showOTPForm() {
        const authContainer = document.getElementById('auth-container');
        const otpContainer = document.getElementById('otp-container');
        
        if (authContainer) authContainer.style.display = 'none';
        if (otpContainer) otpContainer.style.display = 'block';
        
        document.getElementById('otp-email').textContent = this.currentEmail;
        document.querySelector('.otp-input').focus();
    }

    startResendTimer() {
        const resendLink = document.getElementById('resend-otp');
        let seconds = 60;
        
        resendLink.style.pointerEvents = 'none';
        resendLink.style.color = 'var(--slate-grey)';
        
        this.otpResendTimeout = setInterval(() => {
            resendLink.textContent = `Resend OTP (${seconds}s)`;
            seconds--;
            
            if (seconds < 0) {
                clearInterval(this.otpResendTimeout);
                resendLink.textContent = 'Resend OTP';
                resendLink.style.pointerEvents = 'auto';
                resendLink.style.color = 'var(--exun-blue)';
            }
        }, 1000);
    }

    clearOTPInputs() {
        const otpInputs = document.querySelectorAll('.otp-input');
        otpInputs.forEach(input => {
            input.value = '';
        });
        if (otpInputs.length > 0) {
            otpInputs[0].focus();
        }
    }

    validateEmail(email) {
        const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
        return emailRegex.test(email);
    }

    showFieldError(fieldName, message) {
        const field = document.getElementById(fieldName);
        const errorElement = document.getElementById(`${fieldName}-error`);
        
        if (field) {
            field.classList.add('form-input--error');
        }
        
        if (errorElement) {
            errorElement.textContent = message;
        }
    }

    clearErrors() {
        const errorElements = document.querySelectorAll('.form-error');
        const inputElements = document.querySelectorAll('.form-input--error');
        
        errorElements.forEach(el => el.textContent = '');
        inputElements.forEach(el => el.classList.remove('form-input--error'));
    }
}

document.addEventListener('DOMContentLoaded', () => {
    if (document.body.dataset.page === 'login') {
        new LoginPage();
    }
    });