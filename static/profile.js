// static/profile.js
const API_BASE = '/api';

// Fetch user data to populate the profile card
async function loadProfileInfo() {
    try {
        const response = await fetch(`${API_BASE}/check-auth`);
        if (response.ok) {
            const data = await response.json();
            
            // Display profile info
            const profileUsername = document.getElementById('profile-username');
            if (profileUsername) profileUsername.textContent = data.username;
            
            const profileRole = document.getElementById('profile-role');
            if (profileRole) profileRole.textContent = data.is_admin ? 'Administrator' : 'Member';
            
            const profileMember = document.getElementById('profile-member');
            const profileMemberInfo = document.getElementById('profile-member-info');
            
            if (data.rank && profileMember && profileMemberInfo) {
                profileMember.textContent = data.rank;
                profileMemberInfo.style.display = 'block';
            }
        }
    } catch (error) {
        console.error('Failed to load profile info:', error);
    }
}

// Initialize
document.addEventListener('DOMContentLoaded', async () => {
    const passwordForm = document.getElementById('password-form');
    
    // Guard: Only run if we are actually on the Profile page
    if (passwordForm) {
        await loadProfileInfo();
        
        // Change password form submission
        passwordForm.addEventListener('submit', async (e) => {
            e.preventDefault();
            
            const currentPassword = document.getElementById('current-password').value;
            const newPassword = document.getElementById('new-password').value;
            const confirmPassword = document.getElementById('confirm-password').value;
            
            if (newPassword !== confirmPassword) {
                alert('❌ New passwords do not match!');
                return;
            }
            
            if (newPassword.length < 6) {
                alert('❌ New password must be at least 6 characters!');
                return;
            }
            
            try {
                const response = await fetch(`${API_BASE}/change-password`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        current_password: currentPassword,
                        new_password: newPassword
                    })
                });
                
                if (!response.ok) {
                    const error = await response.text();
                    throw new Error(error);
                }
                
                alert('✅ Password changed successfully!');
                passwordForm.reset();
            } catch (error) {
                console.error('Error changing password:', error);
                alert('❌ Failed to change password: ' + error.message);
            }
        });
    }
});