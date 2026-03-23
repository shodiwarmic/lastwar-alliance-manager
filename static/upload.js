// /static/upload.js - Handles the image upload, preview, and processing logic for the Smart Screenshot feature

const API_BASE = '/api';
const MAX_FILES = 100;
let selectedFiles = [];
let manualCategories = {}; // NEW: Track manual overrides

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
        manualCategories = {}; // Reset mappings
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
            // NEW: Append the force category selection
            formData.append('force_category', document.getElementById('force-category').value);
            
            // Append manual mappings if we are retrying
            if (Object.keys(manualCategories).length > 0) {
                formData.append('manual_categories', JSON.stringify(manualCategories));
            }

            const response = await fetch(`${API_BASE}/smart-screenshot`, {
                method: 'POST',
                body: formData
            });

            if (!response.ok) {
                // Intercept the manual categorization request
                if (response.status === 422) {
                    const errData = await response.json();
                    if (errData.error === "requires_manual_categorization") {
                        renderManualCategoryUI(errData.files);
                        processImageBtn.innerHTML = originalText;
                        processImageBtn.disabled = false;
                        return; // Stop the flow
                    }
                }
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
            
            // NEW SUCCESS LOGIC
            currentImportPayload = result;
            currentWeekDate = result.week_date; // Capture the calculated week_date from the server

            // Reset mappings after success
            manualCategories = {};

            // Show any hard OCR chunking errors before popping the modal
            if (result.errors && result.errors.length > 0) {
                showResult(`⚠️ OCR Warning: <br>${result.errors.join('<br>')}`, 'error');
            } else {
                showResult(`✅ OCR Complete. Please review the extracted data in the popup.`, 'success');
            }
            
            renderPreviewModal(result);
            
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
            
            // 1. Check for OCR Pipeline Readiness (both GCP credentials and CV Worker URL must be set)
            if (!settings.ocr_pipeline_ready) {
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

let allMembers = [];
let currentImportPayload = null;
let currentWeekDate = null;

// Fetch members on load so the dropdowns have data
document.addEventListener('DOMContentLoaded', async () => {
    try {
        const response = await fetch('/api/members');
        allMembers = await response.json();
    } catch (error) {
        console.error('Error loading members:', error);
    }
});

function renderPreviewModal(data) {
    const matchedBody = document.getElementById('matched-body');
    const unresolvedBody = document.getElementById('unresolved-body');
    // Ensure you add an <tbody id="ambiguous-body"> into your HTML modal
    const ambiguousBody = document.getElementById('ambiguous-body'); 
    
    matchedBody.innerHTML = '';
    if(unresolvedBody) unresolvedBody.innerHTML = '';
    if(ambiguousBody) ambiguousBody.innerHTML = '';
    
    document.getElementById('matched-count').textContent = data.matched?.length || 0;
    document.getElementById('unresolved-count').textContent = data.unresolved?.length || 0;
    
    // NEW: Count ambiguous if element exists
    const ambigCountEl = document.getElementById('ambiguous-count');
    if(ambigCountEl) ambigCountEl.textContent = data.ambiguous?.length || 0;

    // --- Render Matched ---
    if (data.matched) {
        data.matched.forEach(row => {
            const updates = Object.entries(row.updated_fields).map(([k, v]) => `${k}: ${v}`).join(', ');
            matchedBody.innerHTML += `
                <tr>
                    <td>${row.matched_member.name}</td>
                    <td><span class="badge badge-success">${row.match_type}</span></td>
                    <td>${updates}</td>
                </tr>
            `;
        });
    }

    // Helper to generate dynamic select dropdowns
    const generateReviewRow = (row, idx, bucketType, preSelectedId) => {
        // Use the entire roster for every dropdown, so we can map multiple OCR hallucinations 
        // across different screenshots to the same core member.
        let memberOptions = allMembers.map(m => 
            `<option value="${m.id}" ${m.id === preSelectedId ? 'selected' : ''}>${m.name}</option>`
        ).join('');

        const updates = Object.entries(row.updated_fields).map(([k, v]) => `${k}: ${v}`).join(', ');
        const aliasState = preSelectedId ? "" : "disabled";
        
        return `
            <tr data-index="${idx}" data-bucket="${bucketType}">
                <td><strong>${row.original_name}</strong></td>
                <td>
                    <div style="display: flex; flex-direction: column; gap: 5px;">
                        <select class="member-mapper" onchange="mapUnresolved('${bucketType}', ${idx}, this.value)">
                            <option value="">-- Ignore / Do Not Import --</option>
                            ${memberOptions}
                        </select>
                        <select class="alias-saver" id="alias-save-${bucketType}-${idx}" ${aliasState} style="font-size: 0.85em;">
                            <option value="">Do not save alias</option>
                            <option value="ocr" ${preSelectedId ? 'selected' : ''}>Save as OCR Alias</option>
                            <option value="global">Save as Global Alias</option>
                            <option value="personal">Save as Personal Alias</option>
                        </select>
                    </div>
                </td>
                <td>${updates}</td>
            </tr>
        `;
    };

    // --- Render Ambiguous ---
    if (data.ambiguous && ambiguousBody) {
        data.ambiguous.forEach((row, idx) => {
            ambiguousBody.innerHTML += generateReviewRow(row, idx, 'ambiguous', row.matched_member?.id);
        });
    }

    // --- Render Unresolved ---
    if (data.unresolved && unresolvedBody) {
        data.unresolved.forEach((row, idx) => {
            unresolvedBody.innerHTML += generateReviewRow(row, idx, 'unresolved', null);
        });
    }

    document.getElementById('import-preview-modal').style.display = 'flex';
}

function mapUnresolved(bucketType, unresolvedIndex, memberId) {
    const row = currentImportPayload[bucketType][unresolvedIndex];
    const aliasSelect = document.getElementById(`alias-save-${bucketType}-${unresolvedIndex}`);
    
    if (!memberId) {
        row.matched_member = null;
        aliasSelect.disabled = true;
        aliasSelect.value = "";
    } else {
        row.matched_member = allMembers.find(m => m.id == memberId);
        aliasSelect.disabled = false;
        aliasSelect.value = "ocr"; 
    }
}

function closePreviewModal() {
    document.getElementById('import-preview-modal').style.display = 'none';
    currentImportPayload = null;
}

async function commitImport() {
    const finalRecords = [...(currentImportPayload.matched || [])];
    const saveAliases = [];
    
    // Helper to process a bucket (Ambiguous or Unresolved)
    const processBucket = (bucketType) => {
        if (currentImportPayload[bucketType]) {
            currentImportPayload[bucketType].forEach((row, idx) => {
                if (row.matched_member && row.matched_member.id) {
                    finalRecords.push(row);
                    
                    const aliasSaveType = document.getElementById(`alias-save-${bucketType}-${idx}`).value;
                    if (aliasSaveType) {
                        saveAliases.push({
                            failed_alias: row.original_name,
                            member_id: row.matched_member.id,
                            category: aliasSaveType
                        });
                    }
                }
            });
        }
    };

    processBucket('ambiguous');
    processBucket('unresolved');

    const payload = {
        week_date: currentWeekDate,
        records: finalRecords,
        save_aliases: saveAliases
    };

    try {
        const response = await fetch('/api/vs-points/import/commit', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });

        if (!response.ok) throw new Error(await response.text());
        
        const result = await response.json();
        if (result.errors && result.errors.length > 0) {
            alert(`Backend received ${result.aliases_received} aliases.\n\n${result.message}\n\nErrors:\n${result.errors.join("\n")}`);
        } else {
            alert(result.message);
        }
        
        closePreviewModal();
        
        // Clear files from uploader
        selectedFiles = [];
        document.getElementById('image-input').value = '';
        document.getElementById('preview-container').style.display = 'none';
        document.getElementById('drop-content').style.display = 'block';
        document.getElementById('process-image-btn').style.display = 'none';
        document.getElementById('result-container').innerHTML = '';
        
    } catch (error) {
        alert('Error saving data: ' + error.message);
    }
}

function renderManualCategoryUI(uncategorizedFiles) {
    let html = `
        <div class="result-box result-error">
            <strong>⚠️ Manual Categorization Required</strong><br>
            <p>We couldn't auto-detect the tabs for ${uncategorizedFiles.length} image(s). Please select what they are:</p>
            <div style="margin-top: 10px; max-height: 300px; overflow-y: auto; text-align: left;">
    `;

    uncategorizedFiles.forEach((filename) => {
        html += `
            <div style="margin-bottom: 10px; padding: 10px; background: rgba(0,0,0,0.05); border-radius: 5px;">
                <strong>${filename}</strong><br>
                <select class="manual-cat-select" data-filename="${filename}" style="width: 100%; margin-top: 5px; padding: 5px;">
                    <option value="">-- Select Category --</option>
                    <option value="monday">VS Points: Monday</option>
                    <option value="tuesday">VS Points: Tuesday</option>
                    <option value="wednesday">VS Points: Wednesday</option>
                    <option value="thursday">VS Points: Thursday</option>
                    <option value="friday">VS Points: Friday</option>
                    <option value="saturday">VS Points: Saturday</option>
                    <option value="power">Power Rankings</option>
                    <option value="ignore">Ignore / Skip this image</option>
                </select>
            </div>
        `;
    });

    html += `
            </div>
            <button id="btn-submit-manual-cat" class="btn btn-primary" style="margin-top: 15px; width: 100%;">Save & Continue Processing</button>
        </div>
    `;

    document.getElementById('result-container').innerHTML = html;

    // Handle Retry Submission
    document.getElementById('btn-submit-manual-cat').addEventListener('click', () => {
        const selects = document.querySelectorAll('.manual-cat-select');
        let allFilled = true;
        manualCategories = {};

        selects.forEach(select => {
            if (!select.value) {
                allFilled = false;
                select.style.border = "2px solid red";
            } else {
                select.style.border = "";
                if (select.value !== "ignore") {
                    manualCategories[select.dataset.filename] = select.value;
                }
            }
        });

        if (!allFilled) {
            alert("Please select a category for all images, or choose 'Ignore'.");
            return;
        }

        // Remove ignored files from the payload
        selects.forEach(select => {
             if(select.value === "ignore") {
                  const idx = selectedFiles.findIndex(f => f.name === select.dataset.filename);
                  if(idx > -1) selectedFiles.splice(idx, 1);
             }
        });

        // Trigger the upload button again with the new state
        document.getElementById('process-image-btn').click();
    });
}

