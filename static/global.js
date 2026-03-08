// static/global.js
document.addEventListener('DOMContentLoaded', () => {
    const usernameDisplay = document.getElementById('username-display');
    const logoutBtn = document.getElementById('dropdown-logout-btn');
    
    // Toggle user dropdown menu
    if (usernameDisplay) {
        usernameDisplay.addEventListener('click', (event) => {
            event.stopPropagation();
            const dropdown = document.getElementById('user-dropdown-menu');
            if (dropdown) dropdown.classList.toggle('show');
        });
    }
    
    // Close dropdown when clicking outside
    document.addEventListener('click', (event) => {
        const dropdown = document.getElementById('user-dropdown-menu');
        if (dropdown && usernameDisplay && !usernameDisplay.contains(event.target) && !dropdown.contains(event.target)) {
            dropdown.classList.remove('show');
        }
    });

    // Handle Logout
    if (logoutBtn) {
        logoutBtn.addEventListener('click', async (event) => {
            event.preventDefault();
            if (!confirm('Are you sure you want to logout?')) return;
            
            try {
                await fetch('/api/logout', { method: 'POST' });
                window.location.href = '/login'; 
            } catch (error) {
                console.error('Logout failed:', error);
                window.location.href = '/login';
            }
        });
    }
});