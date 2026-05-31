// Theme Management
const THEME_KEY = 'lastwar-theme-preference';
const THEMES = {
    AUTO: 'auto',
    LIGHT: 'light',
    DARK: 'dark'
};

// Get current theme preference
function getThemePreference() {
    return localStorage.getItem(THEME_KEY) || THEMES.AUTO;
}

// Set theme preference
function setThemePreference(theme) {
    localStorage.setItem(THEME_KEY, theme);
    applyTheme(theme);
}

// Apply theme to document
function applyTheme(theme) {
    const html = document.documentElement;

    // Remove all theme classes
    html.classList.remove('theme-light', 'theme-dark', 'theme-auto');

    // Add the selected theme class
    if (theme === THEMES.LIGHT) {
        html.classList.add('theme-light');
        html.style.colorScheme = 'light';
    } else if (theme === THEMES.DARK) {
        html.classList.add('theme-dark');
        html.style.colorScheme = 'dark';
    } else {
        html.classList.add('theme-auto');
        html.style.colorScheme = 'light dark';
    }

    // Set data-theme attribute for new [data-theme] CSS token blocks
    const resolved = theme === THEMES.AUTO
        ? (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
        : theme;
    html.setAttribute('data-theme', resolved);

    // Notify subscribers (chart/canvas redraws) that the resolved theme changed.
    // Only fires on explicit user switches — page-load callers register AFTER
    // theme.js runs and call their own init functions directly.
    window.dispatchEvent(new CustomEvent('themechange', { detail: { theme: resolved } }));

    // Update theme selector if it exists
    updateThemeSelector(theme);
}

// Update theme selector display
function updateThemeSelector(theme) {
    const themeOptions = document.querySelectorAll('.theme-option');
    themeOptions.forEach(option => {
        const optionTheme = option.dataset.theme;
        if (optionTheme === theme) {
            option.classList.add('active');
        } else {
            option.classList.remove('active');
        }
    });
}

// Initialize theme on page load
function initTheme() {
    const savedTheme = getThemePreference();
    applyTheme(savedTheme);
    
    // Setup theme option click handlers
    document.addEventListener('DOMContentLoaded', () => {
        setupThemeHandlers();
    });
}

// Setup theme option handlers
function setupThemeHandlers() {
    // Re-sync visual state now that the DOM is ready. initTheme() ran before
    // the body was parsed so querySelectorAll found nothing at that point.
    updateThemeSelector(getThemePreference());

    const themeOptions = document.querySelectorAll('.theme-option');
    themeOptions.forEach(option => {
        option.addEventListener('click', (e) => {
            e.preventDefault();
            e.stopPropagation();
            const theme = option.dataset.theme;
            setThemePreference(theme);
        });
    });
}

// Initialize theme immediately
initTheme();

// Re-resolve data-theme when system preference changes while auto is active
window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
    if (getThemePreference() === THEMES.AUTO) applyTheme(THEMES.AUTO);
});
