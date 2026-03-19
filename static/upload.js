const API_BASE = '/api';
const MAX_FILES = 100;
let selectedFiles = [];

document.addEventListener('DOMContentLoaded', async () => {
    const dropZone = document.getElementById('drop-zone');
    if (!dropZone) return;

    const imageInput = document.getElementById('image-input');
    const dropContent = document.getElementById('drop-content');
    const previewContainer = document.getElementById('preview-container');
    const previewGallery = document.getElementById('preview-gallery');
    const filesCount = document.getElementById('files-count');
    const processImageBtn = document.getElementById('process-image-btn');
    const clearBtn = document.getElementById('clear-btn');

    dropZone.addEventListener('click', (e) => {
        if (e.target === clearBtn || clearBtn.contains(e.target)) return;
        if (selectedFiles.length === 0) imageInput.click();
    });

    imageInput.addEventListener('change', (e) => {
        const files = Array.from(e.target.files);
        if (files.length > 0) handleFiles(files);
    });

    dropZone.addEventListener('dragover', (e) => {
        e.preventDefault();
        dropZone.classList.add('dragover');
    });

    dropZone.addEventListener('dragleave', () => {
        dropZone.classList.remove('dragover');
    });

    dropZone.addEventListener('drop', (e) => {
        e.preventDefault();
        dropZone.classList.remove('dragover');
        const files = Array.from(e.dataTransfer.files);
        if (files.length > 0) handleFiles(files);
    });

    function handleFiles(files) {
        let imageFiles = files.filter(file => file.type.startsWith('image/'));
        
        if (imageFiles.length === 0) {
            showResult('Please upload image files (PNG, JPG, JPEG)', 'error');
            return;
        }
        
        const oversizedFiles = imageFiles.filter(file => file.size > 10 * 1024 * 1024);
        if (oversizedFiles.length > 0) {
            showResult(`${oversizedFiles.length} file(s) exceed 10MB limit and will be skipped`, 'error');
            imageFiles = imageFiles.filter(file => file.size <= 10 * 1024 * 1024);
        }
        
        if (selectedFiles.length + imageFiles.length > MAX_FILES) {
            showResult(`Maximum ${MAX_FILES} files allowed. Only adding first ${MAX_FILES - selectedFiles.length} files.`, 'error');
            imageFiles = imageFiles.slice(0, MAX_FILES - selectedFiles.length);
        }
        
        imageFiles.forEach(file => selectedFiles.push(file));
        updatePreview();
        document.getElementById('result-container').innerHTML = '';
    }

    function updatePreview() {
        if (selectedFiles.length === 0) {
            previewContainer.style.display = 'none';
            dropContent.style.display = 'block';
            processImageBtn.style.display = 'none';
            return;
        }
        
        filesCount.textContent = `${selectedFiles.length} file${selectedFiles.length > 1 ? 's' : ''} selected`;
        previewGallery.innerHTML = '';
        
        selectedFiles.forEach((file, index) => {
            const reader = new FileReader();
            reader.onload = (e) => {
                const previewItem = document.createElement('div');
                previewItem.className = 'preview-item';
                previewItem.innerHTML = `
                    <button class="remove-file" data-index="${index}" title="Remove">×</button>
                    <img src="${e.target.result}" class="preview-img" alt="${file.name}">
                    <div class="file-name" title="${file.name}">${file.name}</div>
                `;
                previewItem.querySelector('.remove-file').addEventListener('click', (evt) => {
                    evt.stopPropagation();
                    removeFile(index);
                });
                previewGallery.appendChild(previewItem);
            };
            reader.readAsDataURL(file);
        });
        
        dropContent.style.display = 'none';
        previewContainer.style.display = 'block';
        processImageBtn.style.display = 'block';
    }

    function removeFile(index) {
        selectedFiles.splice(index, 1);
        updatePreview();
    }

    clearBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        selectedFiles = [];
        imageInput.value = '';
        updatePreview();
        document.getElementById('result-container').innerHTML = '';
    });

    processImageBtn.addEventListener('click', async () => {
        if (selectedFiles.length === 0) {
            showResult('Please select at least one image', 'error');
            return;
        }
        
        const originalText = processImageBtn.innerHTML;
        processImageBtn.innerHTML = '<span class="loading"></span> Analyzing & Processing...';
        processImageBtn.disabled = true;
        
        try {
            showResult(`🔍 Compiling ${selectedFiles.length} screenshot(s) for Smart Analysis...`, 'info');
            
            const formData = new FormData();
            selectedFiles.forEach(file => {
                formData.append('images', file);
            });
            formData.append('week', document.getElementById('vs-week').value);
            
            const response = await fetch(`${API_BASE}/smart-screenshot`, {
                method: 'POST',
                body: formData
            });
            
            if (!response.ok) {
                const error = await response.text();
                throw new Error(error);
            }
            
            const result = await response.json();
            
            let html = `<div class="result-box result-success">
                <strong>✅ ${result.message}</strong><br>
                <div style="margin-top: 10px;">
                    <strong>Total Records Updated:</strong> ${result.success_count || 0}`;
                    
            if (result.failed_count > 0) {
                html += ` | <strong>Failed:</strong> ${result.failed_count}`;
            }
            html += `</div>`;
            
            // NEW: Show all the different buckets that were successfully processed
            if (result.processed_groups && result.processed_groups.length > 0) {
                html += `<br><strong>Categories Processed:</strong><br>
                         <ul style="margin: 5px 0 0 20px; font-size: 14px;">
                            ${result.processed_groups.map(group => `<li>${group}</li>`).join('')}
                         </ul>`;
            }
            
            if (result.errors && result.errors.length > 0) {
                html += `<br><br><strong>Issues:</strong><br><div style="max-height: 200px; overflow-y: auto; margin-top: 5px;">${result.errors.join('<br>')}</div>`;
            } else if (result.not_found_members && result.not_found_members.length > 0) {
                html += `<br><br><strong>Issues:</strong><br>${result.not_found_members.length} members not found in database.`;
            }
            
            html += '</div>';
            document.getElementById('result-container').innerHTML = html;
            
            if ((result.success_count || 0) > 0) {
                setTimeout(() => {
                    selectedFiles = [];
                    imageInput.value = '';
                    updatePreview();
                }, 5000);
            }
        } catch (error) {
            console.error('Error processing images:', error);
            showResult(`❌ Processing failed: ${error.message}`, 'error');
        } finally {
            processImageBtn.innerHTML = originalText;
            processImageBtn.disabled = false;
        }
    });

    function showResult(message, type) {
        const resultClass = type === 'error' ? 'result-error' : 
                           type === 'info' ? 'result-info' : 'result-success';
        document.getElementById('result-container').innerHTML = 
            `<div class="result-box ${resultClass}">${message}</div>`;
    }

    // Master Lockout Check
    try {
        const response = await fetch(`${API_BASE}/settings`);
        if (response.ok) {
            const settings = await response.json();
            
            // 1. Check for GCP Credentials
            if (!settings.has_gcp_credentials) {
                document.getElementById('main-upload-container').style.display = 'none';
                document.getElementById('missing-credentials-msg').style.display = 'block';
                return; // Completely abort initializing the rest of the upload logic
            }

            // 2. Warn if Power Tracking is off
            if (!settings.power_tracking_enabled) {
                showResult('⚠️ Power tracking is not enabled globally. Power screenshots will be rejected by the server.', 'info');
            }
        }
    } catch (error) {
        console.error('Failed to check backend configuration:', error);
    }
});