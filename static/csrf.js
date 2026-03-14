document.addEventListener("DOMContentLoaded", () => {
    // --- Bulletproof CSRF Extraction ---
    // gorilla/csrf TemplateField renders a full <input> tag
    const csrfInput = document.querySelector('input[name="gorilla.csrf.Token"]');
    const csrfToken = csrfInput ? csrfInput.value : '';

    if (!csrfToken) {
        console.warn("CSRF Token NOT FOUND. Mutating requests (POST/PUT/DELETE) will fail.");
    }

    // --- Global Fetch Interceptor ---
    const originalFetch = window.fetch;
    window.fetch = async function(...args) {
        let [resource, config] = args;
        config = config || {};

        // Safely identify the HTTP Method
        const method = (config.method || (resource instanceof Request ? resource.method : 'GET')).toUpperCase();

        // Only attach token to data-modifying requests
        if (['POST', 'PUT', 'DELETE'].includes(method)) {
            config.headers = config.headers || {};
            
            // Handle different header formats (Headers object vs Plain object)
            if (config.headers instanceof Headers) {
                config.headers.set('X-CSRF-Token', csrfToken);
            } else {
                config.headers['X-CSRF-Token'] = csrfToken;
            }
            
            args[1] = config;
        }

        return originalFetch(...args);
    };
});