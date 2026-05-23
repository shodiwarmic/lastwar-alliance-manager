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
            option.innerHTML = option.innerHTML.replace('○', '●');
        } else {
            option.classList.remove('active');
            option.innerHTML = option.innerHTML.replace('●', '○');
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
