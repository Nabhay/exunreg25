class EventDetailPage {
    constructor() {
        this.event = null;
        this.eventId = this.getEventIdFromUrl();
        this.init();
    }

    getEventIdFromUrl() {
        const urlParams = new URLSearchParams(window.location.search);
    const q = urlParams.get('id');
    if (q) return q;
    const path = window.location.pathname.replace(/^\//, '');
    return path ? decodeURIComponent(path) : null;
    }

    async init() {
        if (!this.eventId) {
            Utils.showToast('Invalid event ID', 'error');
            Utils.redirect('/events', 800);
            return;
        }

        await this.loadEvent();
        this.renderEvent();
        this.setupEventListeners();
    }

    async loadEvent() {
        try {
            const response = await ExunServices.api.apiRequest(`/events/?id=${encodeURIComponent(this.eventId)}`);
            if (response && response.status === 'success') {
                this.event = response.data;
            } else {
                throw new Error(response && response.error ? response.error : 'Event not found');
            }
        } catch (error) {
            console.error('Failed to load event:', error);
            Utils.showToast('Failed to load event details', 'error');
            Utils.redirect('/events', 800);
        }
    }

    renderEvent() {
        if (!this.event) return;

        this.updatePageTitle();
        this.renderEventHeader();
        this.renderEventDetails();
        this.renderEventDescription();
        this.renderRegistrationSection();
    }

    updatePageTitle() {
        document.title = `${this.event.name} - Exun 2025`;
        
        const breadcrumb = document.querySelector('.breadcrumb__current');
        if (breadcrumb) {
            breadcrumb.textContent = this.event.name;
        }
    }

    renderEventHeader() {
        const titleEl = document.getElementById('event-title');
        const subtitleEl = document.getElementById('event-subtitle');
        if (titleEl) titleEl.textContent = this.event.name;
        if (subtitleEl) subtitleEl.textContent = this.event.subtitle || 'Event Details';

        const imgEl = document.querySelector('.event-detail__image');
        if (imgEl) {
            imgEl.src = this.event.image ? (this.event.image.toString().startsWith('/') ? this.event.image : `/illustrations/${this.event.image.toString().split('/').pop()}`) : '/assets/exun_base.webp';
            imgEl.alt = this.event.name;
        }
    }

    renderEventDetails() {
        const details = document.getElementById('event-info');
        if (!details) return;
        const eligibilityText = Utils.formatEligibility(this.event.eligibility, this.event.open_to_all);
    const registrationType = this.event.individual ? 'Individual' : 'Team';
    const participantsDisplay = this.event.display_participants ? this.event.display_participants : (this.event.participants || 'TBA');

        details.innerHTML = `
            <div class="event-info-item">
                <span class="event-info-label">Mode:</span>
                <span class="event-info-value">${Utils.formatEventMode(this.event.mode)}</span>
            </div>
            <div class="event-info-item">
                <span class="event-info-label">Participants:</span>
                <span class="event-info-value">${participantsDisplay}</span>
            </div>
            <div class="event-info-item">
                <span class="event-info-label">Eligibility:</span>
                <span class="event-info-value">${eligibilityText}</span>
            </div>
            <div class="event-info-item">
                <span class="event-info-label">Points:</span>
                <span class="event-info-value">${this.event.points || 0}</span>
            </div>
            <div class="event-info-item">
                <span class="event-info-label">Dates:</span>
                <span class="event-info-value">${this.event.dates || 'TBA'}</span>
            </div>
            <div class="event-info-item">
                <span class="event-info-label">Registration:</span>
                <span class="event-info-value">${registrationType}</span>
            </div>
        `;
    }

    renderEventDescription() {
    const description = document.getElementById('event-description');
        if (!description) return;
    const longDesc = this.event.description_long || this.event.description_long === '' ? this.event.description_long : null;
    const shortDesc = this.event.description_short || null;

    let html = '<h2>About This Event</h2>';
    html += '<div class="description-content">';
    if (shortDesc) html += `<p class="description-short">${shortDesc}</p>`;
    if (longDesc) html += `<div class="description-long">${this.formatDescription(longDesc)}</div>`;
    if (!shortDesc && !longDesc) html += '<p>Details coming soon.</p>';
    html += '</div>';

    description.innerHTML = html;
    }

    formatDescription(description) {
        return description
            .split('\n')
            .map(paragraph => paragraph.trim())
            .filter(paragraph => paragraph.length > 0)
            .map(paragraph => `<p>${paragraph}</p>`)
            .join('');
    }

    renderRegistrationSection() {
        const registration = document.getElementById('registration-card');
        if (!registration) return;
        const isAuthenticated = ExunServices.api.isAuthenticated();
        registration.innerHTML = `
            <h3>Ready to Register?</h3>
            <div style="margin-top:0.75rem;">
                <button class="btn btn--primary" id="register-event-btn">Register</button>
            </div>
        `;
        const backButtonHtml = `
            <div style="margin-top:0.5rem; display:flex; justify-content:center;" id="back-to-events-wrapper">
                <button class="btn" data-action="back-to-events" id="back-to-events-btn">← Back to events</button>
            </div>
        `;
        const regEl = document.getElementById('registration-card');
        if (regEl && !document.getElementById('back-to-events-btn')) {
            regEl.insertAdjacentHTML('afterend', backButtonHtml);
        }
    }

    setupEventListeners() {
        const registerBtn = document.getElementById('register-event-btn');
        if (registerBtn) {
            registerBtn.addEventListener('click', async () => {
                if (!ExunServices.api.isAuthenticated()) {
                    Utils.redirect('/login', 100);
                    return;
                }

                try {
                    const profileResp = await ExunServices.api.apiRequest('/auth/profile');
                    if (!profileResp || profileResp.status !== 'success' || !profileResp.data) {
                        Utils.redirect('/complete', 100);
                        return;
                    }
                    const user = profileResp.data;
                    const check = this.checkProfileCompleteness(user);
                    if (!check.ok) {
                        const missingList = check.missing.join(', ');
                        Utils.showToast('Complete your profile before registering. Missing: ' + missingList, 'error');
                        setTimeout(() => { Utils.redirect('/complete', 900); }, 900);
                        return;
                    }
                } catch (e) {
                    console.error('Profile check failed', e);
                    Utils.showToast('Failed to verify profile. Please login again.', 'error');
                    Utils.redirect('/login', 200);
                    return;
                }

                await this.openRegistrationModal();
            });
        }

        

        const backBtn = document.querySelector('[data-action="back-to-events"]');
        if (backBtn) backBtn.addEventListener('click', () => Utils.redirect('/events', 200));
    }

    async handleRegistration() {
        try {
            const urlParams = new URLSearchParams(window.location.search);
            const eventId = urlParams.get('id') || this.event.id || this.event.name;
            const eventParticipants = this.event.participants || 1;

            const profileResp = await ExunServices.api.apiRequest('/auth/profile');
            if (!profileResp || profileResp.status !== 'success') {
                Utils.redirect('/login', 100);
                return;
            }
            const user = profileResp.data;
            if (!user) {
                Utils.redirect('/complete', 100);
                return;
            }

            const userIsIndividual = user.individual === true || user.Individual === true;
            if (userIsIndividual && !this.event.independent_registration) {
                Utils.showToast('Individual registration not allowed for this event', 'error');
                return;
            }

            return;
        } catch (error) {
            console.error('Registration failed:', error);
            Utils.showToast(error.message || 'Registration failed. Please try again.', 'error');
            return;
        }

    }

    checkProfileCompleteness(user) {
        const u = Object.assign({}, user);
        const isIndividual = (u.individual === true) || (u.Individual === true) || (u.individual === 'true');
        const missing = [];
        const phone = u.phone_number || u.phoneNumber || u.PhoneNumber || u.Phone || '';
        if (!phone || String(phone).trim() === '') missing.push('phone number');

        if (isIndividual) {
            const fullname = u.fullname || u.fullName || u.Fullname || u.name || '';
            if (!fullname || String(fullname).trim() === '') missing.push('full name');
        } else {
            const inst = u.institution_name || u.institutionName || u.InstitutionName || u.institution || '';
            const princ = u.principals_email || u.principalsEmail || u.PrincipalsEmail || u.principal_email || '';
            if (!inst || String(inst).trim() === '') missing.push('institution name');
            if (!princ || String(princ).trim() === '') missing.push("principal's email");
        }

        return { ok: missing.length === 0, missing };
    }

    async openRegistrationModal() {
    const eventId = this.event.id || this.event.ID || this.eventId || this.event.name;
    let capacity = parseInt(this.event.participants || this.event.capacity || 1, 10) || 1;
    let existingMembers = [];
    const self = this;
    try {
        const summaryResp = await ExunServices.api.apiRequest('/summary');
        if (summaryResp && summaryResp.status === 'success') {
            const events = Array.isArray(summaryResp.data.events) ? summaryResp.data.events : (summaryResp.data && summaryResp.data.events) || [];
            const match = events.find(r => {
                const id = r.eventId || r.eventID || r.event_id || r.event || '';
                const name = r.eventName || r.event_name || r.EventName || r.event || '';
                const slug = String(name || id).toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/(^-|-$)/g, '');
                const currentSlug = String(this.event.name || eventId || '').toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/(^-|-$)/g, '');
                return String(id) === String(eventId) || slug === currentSlug || String(id) === String(currentSlug);
            });
            if (match) {
                capacity = parseInt(match.capacity || match.Capacity || this.event.participants || 1, 10) || capacity;
                existingMembers = (match.teamMembers && match.teamMembers.length > 0) ? match.teamMembers : (match.participants && match.participants.length > 0 ? match.participants : []);
            }
        }
    } catch (e) {
    }

    const overlay = document.createElement('div');
    overlay.className = 'modal-overlay';
    overlay.tabIndex = -1;

    const modal = document.createElement('div');
    modal.className = 'modal-box';

    const title = document.createElement('h3');
    title.textContent = `Register for ${this.event.name || ''}`;
    title.style.marginTop = '0';
    modal.appendChild(title);

    const editor = document.createElement('div');
    editor.className = 'inline-registration-editor summary-inline-animate';
    editor.setAttribute('role', 'region');
    editor.setAttribute('aria-label', `Register for ${this.event.name || ''}`);
    editor.style.marginTop = '12px';
    editor.style.padding = '14px 8px 0px';
    editor.style.borderTop = '1px solid rgba(0,0,0,0.06)';

    const rows = [];
    const initialData = [];

    const closeEditorSafely = () => { try { if (typeof cleanup === 'function') cleanup(); } catch(e) {} };
    const createRow = (p) => {
        const row = document.createElement('div');
        row.className = 'inline-member-row';
        row.style.display = 'grid';
        row.style.gridTemplateColumns = '1fr 1fr 90px 130px auto';
        row.style.gap = '12px';
        row.style.marginBottom = '10px';
        const nameVal = String(p && (p.name||p.Name) || '');
        const emailVal = String(p && (p.email||p.Email) || '');
        const classVal = String(p && (p.class||p.Class) || '');
        const phoneVal = String(p && (p.phone||p.Phone) || '');
        row.innerHTML = `
            <input class="form-input" data-name="name" placeholder="Full name" value="${nameVal.replace(/\"/g,'&quot;')}" />
            <input class="form-input" data-name="email" placeholder="Email" value="${emailVal.replace(/\"/g,'&quot;')}" />
            <input class="form-input" data-name="class" placeholder="Class" value="${classVal.replace(/\"/g,'&quot;')}" />
            <input class="form-input" data-name="phone" placeholder="Phone" value="${phoneVal.replace(/\"/g,'&quot;')}" />
            <button class="btn btn--tertiary btn-inline-clear"><svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="lucide lucide-delete-icon lucide-delete"><path d="M10 5a2 2 0 0 0-1.344.519l-6.328 5.74a1 1 0 0 0 0 1.481l6.328 5.741A2 2 0 0 0 10 19h10a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2z"/><path d="m12 9 6 6"/><path d="m18 9-6 6"/></svg></button>
        `;
        const clearBtn = row.querySelector('.btn-inline-clear');
            clearBtn.addEventListener('click', async (e) => {
            e.stopPropagation();
                    if (rows.length <= 1) {
                    const confirmed = await (window.Utils && window.Utils.showConfirmModal ? window.Utils.showConfirmModal('Delete registration? This will permanently remove your registration for this event. Continue?', 'Delete registration', 'Delete', 'Cancel') : Promise.resolve(confirm('Delete registration? This will permanently remove your registration for this event. Continue?')));
                    if (!confirmed) return;
                    try {
                        const resp = await fetch('/api/submit_registrations', { method: 'POST', headers: { 'Content-Type': 'application/json' }, credentials: 'include', body: JSON.stringify({ id: eventId, action: 'delete' }) });
                        let json = null;
                        try { json = await resp.json(); } catch(e) { json = null; }
                        if (resp.ok && (json === true || (json && json.status === 'success'))) {
                            Utils.showToast('Registration deleted', 'success');
                            setTimeout(() => { try { cleanup(); self.renderRegistrationSection(); } catch(e) { cleanup(); } }, 400);
                        } else {
                            const serverMsg = (json && (json.error || json.message || json.msg)) ? (json.error || json.message || json.msg) : null;
                            if (serverMsg) Utils.showToast(serverMsg.toString(), 'error');
                            else Utils.showToast('Delete failed', 'error');
                        }
                    } catch (err) {
                        Utils.showToast((err && err.message) ? err.message : 'Delete failed', 'error');
                    }
                    closeEditorSafely();
                    return;
                }
            const idx = rows.indexOf(row);
            if (idx !== -1) rows.splice(idx, 1);
            row.remove();
            if (addBtn) addBtn.disabled = (rows.length >= capacity);
        });
        return row;
    };

    const addBtn = document.createElement('button');
    addBtn.className = 'btn btn--tertiary';
    addBtn.textContent = 'Add participant';
    addBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        if (rows.length >= capacity) return;
        const existingIndex = rows.length;
        const p = existingMembers[existingIndex] || {};
        const nr = createRow(p);
        rows.push(nr);
        if (addContainer) editor.insertBefore(nr, addContainer);
        addBtn.disabled = (rows.length >= capacity);
        setTimeout(() => { const first = nr.querySelector('input[data-name="name"]'); if (first) first.focus(); }, 20);
    });

    const initialCount = Math.max(1, Math.min(existingMembers.length, capacity));
    for (let i = 0; i < initialCount; i++) {
        const p = existingMembers[i] || {};
        const r = createRow(p);
        rows.push(r);
        editor.appendChild(r);
        initialData.push({ name: r.querySelector('[data-name="name"]').value || '', email: r.querySelector('[data-name="email"]').value || '', class: r.querySelector('[data-name="class"]').value || '', phone: r.querySelector('[data-name="phone"]').value || '' });
    }
    editor.__initialData = JSON.stringify(initialData);

    const addContainer = document.createElement('div');
    addContainer.style.marginTop = '8px';
    addContainer.style.display = 'flex';
    addContainer.style.justifyContent = 'flex-start';
    addContainer.appendChild(addBtn);
    editor.appendChild(addContainer);
    if (addBtn) addBtn.disabled = (rows.length >= capacity);

    const actions = document.createElement('div');
    actions.style.marginTop = '6px';
    actions.style.display = 'flex';
    actions.style.gap = '12px';
    actions.style.justifyContent = 'flex-end';

    const saveBtn = document.createElement('button');
    saveBtn.className = 'btn btn--primary';
    saveBtn.textContent = 'Save';
    const cancelBtn = document.createElement('button');
    cancelBtn.className = 'btn btn--secondary';
    cancelBtn.textContent = 'Cancel';
    if (addBtn) editor.appendChild(addBtn);
    actions.appendChild(saveBtn);
    actions.appendChild(cancelBtn);
    editor.appendChild(actions);

    modal.appendChild(editor);
    overlay.appendChild(modal);
    document.body.appendChild(overlay);

    setTimeout(() => {
        overlay.classList.add('modal-open');
        modal.classList.add('modal-open');
    }, 10);

    const cleanup = () => {
        overlay.classList.remove('modal-open');
        modal.classList.remove('modal-open');
        overlay.classList.add('modal-closing');
        modal.classList.add('modal-closing');
        setTimeout(() => { overlay.remove(); document.removeEventListener('keydown', onKeyDown); }, 260);
    };

    const onKeyDown = (ev) => { if (ev.key === 'Escape') cleanup(); };
    document.addEventListener('keydown', onKeyDown);

    cancelBtn.addEventListener('click', (e) => { e.stopPropagation(); cleanup(); });
    overlay.addEventListener('click', (ev) => { if (ev.target === overlay) cleanup(); });

    saveBtn.addEventListener('click', async (e) => {
        e.stopPropagation();
        const data = [];
        for (const r of rows) {
            const name = ((r.querySelector('[data-name="name"]')||{value:''}).value || '').trim();
            const email = ((r.querySelector('[data-name="email"]')||{value:''}).value || '').trim();
            const cls = ((r.querySelector('[data-name="class"]')||{value:''}).value || '').trim();
            const phone = ((r.querySelector('[data-name="phone"]')||{value:''}).value || '').trim();
            if (!name) continue;
            data.push({ name, email, class: parseInt(cls||0,10) || cls, phone });
        }
        if (data.length === 0) { Utils.showToast('Please add at least one participant', 'error'); return; }
        try {
            const resp = await fetch('/api/submit_registrations', { method: 'POST', headers: { 'Content-Type': 'application/json' }, credentials: 'include', body: JSON.stringify({ id: eventId, data }) });
            let json = null;
            try { json = await resp.json(); } catch(e) { json = null; }
            if (resp.ok && (json === true || (json && json.status === 'success'))) {
                Utils.showToast('Registration saved', 'success');
                setTimeout(() => { try { cleanup(); self.renderRegistrationSection(); } catch(e) { cleanup(); } }, 800);
            } else {
                const serverMsg = (json && (json.error || json.message || json.msg)) ? (json.error || json.message || json.msg) : null;
                if (serverMsg) Utils.showToast(serverMsg.toString(), 'error');
                else Utils.showToast('Save failed', 'error');
            }
        } catch (err) {
            console.error('Save failed', err);
            const msg = (err && err.message) ? err.message : 'Save failed';
            Utils.showToast(msg, 'error');
        }
    });

    setTimeout(() => { const first = editor.querySelector('input[data-name="name"]'); if (first) first.focus(); }, 50);
}
    
}

function isAuthenticatedUser() {
    return ExunServices.api.isAuthenticated();
}

document.addEventListener('DOMContentLoaded', async () => {
    if (document.body.dataset.page === 'event-detail') {
        await Utils.loadComponent('components/navbar.html', document.getElementById('navbar-container'));
        await Utils.loadComponent('components/footer.html', document.getElementById('footer-container'));
        new EventDetailPage();
    }
});
