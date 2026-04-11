// /static/upload.js - Handles the image upload, preview, and processing logic for the Smart Screenshot feature

const API_BASE = '/api';
const MAX_FILES = 100;
let selectedFiles = [];
let manualCategories = {}; // Track manual overrides

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
        document.getElementById('result-container').replaceChildren();
    }

    function updatePreview() {
        if (selectedFiles.length === 0) {
            previewContainer.style.display = 'none';
            dropContent.style.display = 'block';
            processImageBtn.style.display = 'none';
            return;
        }

        filesCount.textContent = `${selectedFiles.length} file${selectedFiles.length > 1 ? 's' : ''} selected`;
        previewGallery.replaceChildren();

        selectedFiles.forEach((file, index) => {
            const reader = new FileReader();
            reader.onload = (e) => {
                const previewItem = document.createElement('div');
                previewItem.className = 'preview-item';

                const removeBtn = document.createElement('button');
                removeBtn.className = 'remove-file';
                removeBtn.dataset.index = index;
                removeBtn.title = 'Remove';
                removeBtn.textContent = '×';
                removeBtn.addEventListener('click', (evt) => {
                    evt.stopPropagation();
                    removeFile(index);
                });

                const img = document.createElement('img');
                img.src = e.target.result;
                img.className = 'preview-img';
                img.alt = file.name;

                const nameDiv = document.createElement('div');
                nameDiv.className = 'file-name';
                nameDiv.title = file.name;
                nameDiv.textContent = file.name;

                previewItem.append(removeBtn, img, nameDiv);
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
        document.getElementById('result-container').replaceChildren();
    });

    processImageBtn.addEventListener('click', async () => {
        if (selectedFiles.length === 0) {
            showResult('Please select at least one image', 'error');
            return;
        }

        const originalText = processImageBtn.textContent;
        const loadSpan = document.createElement('span');
        loadSpan.className = 'loading';
        processImageBtn.replaceChildren(loadSpan, document.createTextNode(' Analyzing & Processing...'));
        processImageBtn.disabled = true;

        try {
            showResult(`🔍 Compiling ${selectedFiles.length} screenshot(s) for Smart Analysis...`, 'info');

            const formData = new FormData();
            selectedFiles.forEach(file => {
                formData.append('images', file);
            });
            formData.append('week', document.getElementById('vs-week').value);
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
                        processImageBtn.textContent = originalText;
                        processImageBtn.disabled = false;
                        return; // Stop the flow
                    }
                }
                const error = await response.text();
                throw new Error(error);
            }

            const result = await response.json();

            // Build result box with DOM
            const resultBox = document.createElement('div');
            resultBox.className = 'result-box result-success';

            const strong = document.createElement('strong');
            strong.textContent = `✅ ${result.message}`;
            resultBox.appendChild(strong);
            resultBox.appendChild(document.createElement('br'));

            const countDiv = document.createElement('div');
            countDiv.style.marginTop = '10px';
            const countStrong = document.createElement('strong');
            countStrong.textContent = 'Total Records Updated:';
            countDiv.append(countStrong, ` ${result.success_count || 0}`);
            if (result.failed_count > 0) {
                const failedStrong = document.createElement('strong');
                failedStrong.textContent = ' | Failed:';
                countDiv.append(failedStrong, ` ${result.failed_count}`);
            }
            resultBox.appendChild(countDiv);

            if (result.processed_groups && result.processed_groups.length > 0) {
                resultBox.appendChild(document.createElement('br'));
                const groupLabel = document.createElement('strong');
                groupLabel.textContent = 'Categories Processed:';
                resultBox.appendChild(groupLabel);
                resultBox.appendChild(document.createElement('br'));
                const ul = document.createElement('ul');
                ul.style.cssText = 'margin: 5px 0 0 20px; font-size: 14px;';
                result.processed_groups.forEach(group => {
                    const li = document.createElement('li');
                    li.textContent = group;
                    ul.appendChild(li);
                });
                resultBox.appendChild(ul);
            }

            resultBox.appendChild(document.createElement('div')); // closing div placeholder
            document.getElementById('result-container').replaceChildren(resultBox);

            currentImportPayload = result;
            currentWeekDate = result.week_date;
            manualCategories = {};

            // Show any hard OCR chunking errors before popping the modal
            if (result.errors && result.errors.length > 0) {
                showResultMultiline('⚠️ OCR Warning:', result.errors, 'error');
            } else {
                showResult('✅ OCR Complete. Please review the extracted data in the popup.', 'success');
            }

            renderPreviewModal(result);

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
            processImageBtn.textContent = originalText;
            processImageBtn.disabled = false;
        }
    });

    function showResult(message, type) {
        const resultClass = type === 'error' ? 'result-error' :
                           type === 'info' ? 'result-info' : 'result-success';
        const div = document.createElement('div');
        div.className = `result-box ${resultClass}`;
        div.textContent = message;
        document.getElementById('result-container').replaceChildren(div);
    }

    function showResultMultiline(label, lines, type) {
        const resultClass = type === 'error' ? 'result-error' :
                           type === 'info' ? 'result-info' : 'result-success';
        const div = document.createElement('div');
        div.className = `result-box ${resultClass}`;
        const labelEl = document.createElement('strong');
        labelEl.textContent = label;
        div.appendChild(labelEl);
        lines.forEach(line => {
            div.appendChild(document.createElement('br'));
            div.appendChild(document.createTextNode(line));
        });
        document.getElementById('result-container').replaceChildren(div);
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

function buildMatchedRow(row) {
    const updates = Object.entries(row.updated_fields).map(([k, v]) => `${k}: ${v}`).join(', ');

    const tr = document.createElement('tr');

    const tdName = document.createElement('td');
    tdName.textContent = row.matched_member.name;

    const tdType = document.createElement('td');
    const badge = document.createElement('span');
    badge.className = 'badge badge-success';
    badge.textContent = row.match_type;
    tdType.appendChild(badge);

    const tdUpdates = document.createElement('td');
    tdUpdates.textContent = updates;

    tr.append(tdName, tdType, tdUpdates);
    return tr;
}

function buildReviewRow(row, idx, bucketType, preSelectedId) {
    const updates = Object.entries(row.updated_fields).map(([k, v]) => `${k}: ${v}`).join(', ');

    const tr = document.createElement('tr');
    tr.dataset.index = idx;
    tr.dataset.bucket = bucketType;

    const tdName = document.createElement('td');
    const strong = document.createElement('strong');
    strong.textContent = row.original_name;
    tdName.appendChild(strong);

    const tdMap = document.createElement('td');
    const wrapper = document.createElement('div');
    wrapper.style.cssText = 'display: flex; flex-direction: column; gap: 5px;';

    const memberSelect = document.createElement('select');
    memberSelect.className = 'member-mapper';
    const ignoreOpt = document.createElement('option');
    ignoreOpt.value = '';
    ignoreOpt.textContent = '-- Ignore / Do Not Import --';
    memberSelect.appendChild(ignoreOpt);
    allMembers.forEach(m => {
        const opt = document.createElement('option');
        opt.value = m.id;
        opt.textContent = m.name;
        if (m.id === preSelectedId) opt.selected = true;
        memberSelect.appendChild(opt);
    });
    memberSelect.addEventListener('change', (e) => mapUnresolved(bucketType, idx, e.target.value));

    const aliasSelect = document.createElement('select');
    aliasSelect.className = 'alias-saver';
    aliasSelect.id = `alias-save-${bucketType}-${idx}`;
    aliasSelect.disabled = !preSelectedId;
    aliasSelect.style.fontSize = '0.85em';
    [
        ['', 'Do not save alias'],
        ['ocr', 'Save as OCR Alias'],
        ['global', 'Save as Global Alias'],
        ['personal', 'Save as Personal Alias'],
    ].forEach(([val, text]) => {
        const opt = document.createElement('option');
        opt.value = val;
        opt.textContent = text;
        if (val === 'ocr' && preSelectedId) opt.selected = true;
        aliasSelect.appendChild(opt);
    });

    wrapper.append(memberSelect, aliasSelect);
    tdMap.appendChild(wrapper);

    const tdUpdates = document.createElement('td');
    tdUpdates.textContent = updates;

    tr.append(tdName, tdMap, tdUpdates);
    return tr;
}

function renderPreviewModal(data) {
    const matchedBody = document.getElementById('matched-body');
    const unresolvedBody = document.getElementById('unresolved-body');
    const ambiguousBody = document.getElementById('ambiguous-body');

    matchedBody.replaceChildren();
    if (unresolvedBody) unresolvedBody.replaceChildren();
    if (ambiguousBody) ambiguousBody.replaceChildren();

    document.getElementById('matched-count').textContent = data.matched?.length || 0;
    document.getElementById('unresolved-count').textContent = data.unresolved?.length || 0;

    const ambigCountEl = document.getElementById('ambiguous-count');
    if (ambigCountEl) ambigCountEl.textContent = data.ambiguous?.length || 0;

    // Render Matched
    if (data.matched) {
        data.matched.forEach(row => matchedBody.appendChild(buildMatchedRow(row)));
    }

    // Render Ambiguous
    if (data.ambiguous && ambiguousBody) {
        data.ambiguous.forEach((row, idx) => {
            ambiguousBody.appendChild(buildReviewRow(row, idx, 'ambiguous', row.matched_member?.id));
        });
    }

    // Render Unresolved
    if (data.unresolved && unresolvedBody) {
        data.unresolved.forEach((row, idx) => {
            unresolvedBody.appendChild(buildReviewRow(row, idx, 'unresolved', null));
        });
    }

    const previewModal = document.getElementById('import-preview-modal');
    previewModal.style.display = 'flex';
    trapFocus(previewModal);
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
    const previewModal = document.getElementById('import-preview-modal');
    releaseFocus(previewModal);
    previewModal.style.display = 'none';
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
            console.error('Import errors:', result.errors);
            showToast(result.message + ` (${result.errors.length} error(s) — see console)`, 'error');
        } else {
            showToast(result.message || 'Import complete.');
        }

        closePreviewModal();

        // Clear files from uploader
        selectedFiles = [];
        document.getElementById('image-input').value = '';
        document.getElementById('preview-container').style.display = 'none';
        document.getElementById('drop-content').style.display = 'block';
        document.getElementById('process-image-btn').style.display = 'none';
        document.getElementById('result-container').replaceChildren();

    } catch (error) {
        showToast('Error saving data: ' + error.message, 'error');
    }
}

function renderManualCategoryUI(uncategorizedFiles) {
    const outerBox = document.createElement('div');
    outerBox.className = 'result-box result-error';

    const heading = document.createElement('strong');
    heading.textContent = '⚠️ Manual Categorization Required';
    outerBox.appendChild(heading);
    outerBox.appendChild(document.createElement('br'));

    const desc = document.createElement('p');
    desc.textContent = `We couldn't auto-detect the tabs for ${uncategorizedFiles.length} image(s). Please select what they are:`;
    outerBox.appendChild(desc);

    const scrollDiv = document.createElement('div');
    scrollDiv.style.cssText = 'margin-top: 10px; max-height: 300px; overflow-y: auto; text-align: left;';

    uncategorizedFiles.forEach(filename => {
        const itemDiv = document.createElement('div');
        itemDiv.style.cssText = 'margin-bottom: 10px; padding: 10px; background: rgba(0,0,0,0.05); border-radius: 5px;';

        const nameEl = document.createElement('strong');
        nameEl.textContent = filename;
        itemDiv.appendChild(nameEl);
        itemDiv.appendChild(document.createElement('br'));

        const select = document.createElement('select');
        select.className = 'manual-cat-select';
        select.dataset.filename = filename;
        select.style.cssText = 'width: 100%; margin-top: 5px; padding: 5px;';

        [
            ['', '-- Select Category --'],
            ['monday', 'VS Points: Monday'],
            ['tuesday', 'VS Points: Tuesday'],
            ['wednesday', 'VS Points: Wednesday'],
            ['thursday', 'VS Points: Thursday'],
            ['friday', 'VS Points: Friday'],
            ['saturday', 'VS Points: Saturday'],
            ['power', 'Power Rankings'],
            ['ignore', 'Ignore / Skip this image'],
        ].forEach(([val, text]) => {
            const opt = document.createElement('option');
            opt.value = val;
            opt.textContent = text;
            select.appendChild(opt);
        });

        itemDiv.appendChild(select);
        scrollDiv.appendChild(itemDiv);
    });

    outerBox.appendChild(scrollDiv);

    const submitBtn = document.createElement('button');
    submitBtn.id = 'btn-submit-manual-cat';
    submitBtn.className = 'btn btn-primary';
    submitBtn.style.cssText = 'margin-top: 15px; width: 100%;';
    submitBtn.textContent = 'Save & Continue Processing';
    outerBox.appendChild(submitBtn);

    document.getElementById('result-container').replaceChildren(outerBox);

    // Handle Retry Submission
    submitBtn.addEventListener('click', () => {
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
            showToast("Please select a category for all images, or choose 'Ignore'.", 'error');
            return;
        }

        // Remove ignored files from the payload
        selects.forEach(select => {
            if (select.value === "ignore") {
                const idx = selectedFiles.findIndex(f => f.name === select.dataset.filename);
                if (idx > -1) selectedFiles.splice(idx, 1);
            }
        });

        // Trigger the upload button again with the new state
        document.getElementById('process-image-btn').click();
    });
}
