class EventsPage {
    constructor() {
        this.events = [];
        this.filteredEvents = [];
        this.currentFilter = 'all';
        this.init();
    }

    async init() {
        await this.loadEvents();
        this.setupFilters();
        this.renderEvents();
        this.setupEventListeners();
    }

    async loadEvents() {
        console.log('EventsPage: loadEvents starting');
        try {
            const response = await ExunServices.api.apiRequest('/events');
            console.log('EventsPage: /api/events response', response && response.data && response.data.length);
            this.events = response.data || [];
            this.events = this.events.map(event => ({
                ...event,
                    image: event.image ? (event.image.toString().startsWith('/') ? event.image : `/illustrations/${event.image.toString().split('/').pop()}`) : '/assets/exun_base.webp'
            }));
            this.filteredEvents = [...this.events];
        } catch (error) {
            console.error('Failed to load events from API:', error);
            Utils.showToast('Failed to load events', 'error');
            this.showNoEvents();
        }
    }

    setupFilters() {
        const filtersContainer = document.querySelector('.events-page__filters');
        if (!filtersContainer) return;

        const categories = this.getEventCategories();
        
        filtersContainer.innerHTML = '';
        
        const allFilter = this.createFilterButton('all', 'All Events', true);
        filtersContainer.appendChild(allFilter);
        
        categories.forEach(category => {
            const filterBtn = this.createFilterButton(category.key, category.name);
            filtersContainer.appendChild(filterBtn);
        });
    }

    createFilterButton(key, name, active = false) {
        const button = document.createElement('button');
        button.className = `filter-btn ${active ? 'filter-btn--active' : ''}`;
        button.textContent = name;
        button.dataset.filter = key;
        
        button.addEventListener('click', () => {
            this.applyFilter(key);
            this.updateActiveFilter(button);
        });
        
        return button;
    }

    getEventCategories() {
        const categories = new Set();
        
        this.events.forEach(event => {
            if (event.category) {
                categories.add(event.category);
            }
            
            if (event.name.includes('Build:')) {
                categories.add('build');
            } else if (event.name.includes('CubXL')) {
                categories.add('cubing');
            } else if (event.name.includes('DomainSquare+')) {
                categories.add('gaming');
            } else if (event.name.includes('CyberX')) {
                categories.add('cybersec');
            } else if (event.name.includes('Roboknights')) {
                categories.add('robotics');
            }
        });
        
        return [
            { key: 'programming', name: 'Programming' },
            { key: 'build', name: 'Build Events' },
            { key: 'gaming', name: 'Gaming' },
            { key: 'cubing', name: 'Cubing' },
            { key: 'cybersec', name: 'Cybersecurity' },
            { key: 'robotics', name: 'Robotics' },
            { key: 'quiz', name: 'Quiz Events' }
        ].filter(cat => categories.has(cat.key));
    }

    applyFilter(filterKey) {
        this.currentFilter = filterKey;
        
        if (filterKey === 'all') {
            this.filteredEvents = [...this.events];
        } else {
            this.filteredEvents = this.events.filter(event => {
                if (event.category === filterKey) return true;
                
                switch (filterKey) {
                    case 'build':
                        return event.name.includes('Build:');
                    case 'cubing':
                        return event.name.includes('CubXL');
                    case 'gaming':
                        return event.name.includes('DomainSquare+') || event.name.includes('Gaming');
                    case 'cybersec':
                        return event.name.includes('CyberX');
                    case 'robotics':
                        return event.name.includes('Roboknights');
                    case 'programming':
                        return ['Competitive Programming', 'Sudocrypt', 'Turing Test'].includes(event.name);
                    case 'quiz':
                        return event.name.toLowerCase().includes('quiz') || event.name === 'Crossword';
                    default:
                        return false;
                }
            });
        }
        
        this.renderEvents();
    }

    updateActiveFilter(activeButton) {
        const filterButtons = document.querySelectorAll('.filter-btn');
        filterButtons.forEach(btn => btn.classList.remove('filter-btn--active'));
        activeButton.classList.add('filter-btn--active');
    }

    renderEvents() {
        const eventsGrid = document.querySelector('.events-page__grid');
        if (!eventsGrid) return;

        eventsGrid.innerHTML = '';

        if (this.filteredEvents.length === 0) {
            this.showNoEvents();
            return;
        }

        this.filteredEvents.forEach(event => {
            const eventCard = this.createEventCard(event);
            eventsGrid.appendChild(eventCard);
        });
    }

    createEventCard(event) {
    const card = document.createElement('a');
    card.href = `/${slugify(event.name)}`;
        card.className = 'event-card';
        card.onclick = (e) => {
            e.preventDefault();
            this.navigateToEvent(event.name);
        };

    const imageUrl = event.image ? event.image : '/assets/exun_base.webp';
        const eligibilityText = Utils.formatEligibility(event.eligibility, event.open_to_all);
    const participantsText = event.display_participants ? event.display_participants : Utils.formatParticipants(event.participants);
        const modeText = Utils.formatEventMode(event.mode);

        card.innerHTML = `
            <img src="${imageUrl}" alt="${event.name}" class="event-card__image" />
            <h3 class="event-card__title">${event.name}</h3>
            <div class="event-card__details">
                <span class="event-card__mode">${modeText}</span>
                <span class="event-card__participants">${participantsText}</span>
                <span class="event-card__eligibility ${event.open_to_all ? 'event-card__eligibility--open' : ''}">${eligibilityText}</span>
            </div>
        `;

        return card;
    }

    showNoEvents() {
        const eventsGrid = document.querySelector('.events-page__grid');
        if (!eventsGrid) return;
        
        eventsGrid.innerHTML = `
            <div class="no-events">
                No events found for the selected filter.
            </div>
        `;
    }

    setupEventListeners() {
        const searchInput = document.querySelector('[data-search="events"]');
        if (searchInput) {
            searchInput.addEventListener('input', debounce((e) => {
                this.searchEvents(e.target.value);
            }, 300));
        }
    }

    searchEvents(query) {
        if (!query.trim()) {
            this.applyFilter(this.currentFilter);
            return;
        }

        const searchTerm = query.toLowerCase();
        this.filteredEvents = this.events.filter(event => 
            event.name.toLowerCase().includes(searchTerm) ||
            (event.description_short && event.description_short.toLowerCase().includes(searchTerm)) ||
            (event.description_long && event.description_long.toLowerCase().includes(searchTerm))
        );
        
        this.renderEvents();
    }

    navigateToEvent(eventName) {
    window.location.href = `/${slugify(eventName)}`;
    }
}

function slugify(text) {
    return String(text)
    .trim()
    .toLowerCase()
    .replace(/[:'/"\?\!\.,]+/g, '')
    .replace(/\s+/g, '-')
    .replace(/[^a-z0-9\-]/g, '')
    .replace(/-+/g, '-');
}

document.addEventListener('DOMContentLoaded', () => {
    if (document.body.dataset.page === 'events') {
        new EventsPage();
    }
});
