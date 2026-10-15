// Officer-created plans; completed history remains train_logs.
'use strict';
window.TrainSchedule = (() => {
    const config = document.getElementById('page-config').dataset;
    const canView = config.canViewSchedule === 'true';
    const canManage = config.canManageSchedule === 'true';
    const canComplete = canManage && config.canManage === 'true';
    let entries = [], today = '', loading = false, opening = false;
    let mode = 'create', target = null, conductor = null, guest = null, members = [], pickers = [];
    const node = id => document.getElementById(id);
    function nameNode(text) { const span = document.createElement('span'); span.textContent = text; return noTranslate(span); }
    const guestType = type => type === 'GUARDIAN_DEFENDER' ? 'Guardian Defender' : 'Special Guest';
    function cell(row, text) { const td = row.insertCell(); td.textContent = text; return td; }
    function message(text) {
        const p = document.createElement('p'); p.className = 'empty-state'; p.textContent = text;
        node('schedule-container').replaceChildren(p);
    }
    async function load() {
        if (!canView || loading) return;
        loading = true;
        node('schedule-refresh').disabled = true;
        message('Loading…');
        try {
            const res = await fetch('/api/train-schedule');
            if (!res.ok) throw new Error('HTTP ' + res.status);
            const data = await res.json();
            entries = data.entries; today = data.game_date;
            render();
        } catch (err) {
            console.error('loadTrainSchedule:', err);
            message('Could not load upcoming trains. Use Refresh to try again.');
        } finally { loading = false; node('schedule-refresh').disabled = false; }
    }
    function render() {
        if (!entries.length) { message('No upcoming or overdue trains planned.'); return; }
        const table = document.createElement('table'); table.className = 'data-table train-schedule-table';
        const head = table.createTHead().insertRow();
        ['Date', 'Type', 'Planned conductor', 'Planned guest', 'Status', 'Plan notes', ...(canManage ? ['Actions'] : [])].forEach(text => {
            const th = document.createElement('th'); th.scope = 'col'; th.textContent = text; head.appendChild(th);
        });
        const body = table.createTBody();
        entries.forEach(s => {
            const row = body.insertRow(); row.dataset.search = [s.conductor_name, s.vip_name, s.notes].join(' ');
            cell(row, s.date);
            cell(row, s.train_type === 'FREE' ? 'Free' : 'Purchased');
            noTranslate(cell(row, s.conductor_name));
            const vip = cell(row, '');
            if (s.vip_name) {
                const name = document.createElement('span'); name.textContent = s.vip_name; noTranslate(name); vip.appendChild(name);
                const type = document.createElement('span'); type.className = 'badge-vip-type'; type.textContent = guestType(s.vip_type); vip.appendChild(type);
            } else { vip.textContent = '—'; }
            const state = s.status === 'planned' && s.date < today ? 'overdue' : s.status;
            const badge = document.createElement('span'); badge.className = 'train-schedule-status train-schedule-status--' + state;
            badge.textContent = state[0].toUpperCase() + state.slice(1); cell(row, '').appendChild(badge);
            const notes = cell(row, s.notes || '—'); if (s.notes) TranslateBlock.attach(notes, s.notes);
            if (canManage) {
                const actions = document.createElement('div'); actions.className = 'row-actions';
                if (s.status === 'planned') {
                    if (s.date >= today) actions.appendChild(rowActionBtn('btn btn-secondary btn-sm', 'edit', 'Edit', () => open(s, 'edit')));
                    actions.appendChild(rowActionBtn('btn btn-secondary btn-sm', 'x', 'Cancel', () => cancel(s)));
                    if (canComplete && s.date <= today) actions.appendChild(rowActionBtn('btn btn-primary btn-sm', 'check', 'Complete', () => open(s, 'complete')));
                }
                cell(row, '').appendChild(actions);
            }
        });
        const wrap = document.createElement('div'); wrap.className = 'table-scroll'; wrap.appendChild(table);
        node('schedule-container').replaceChildren(wrap);
        QuickSearch.apply('schedule-search');
    }
    function showSelections() {
        node('schedule-conductor-selected').textContent = conductor?.name || 'Choose a current alliance member';
        node('schedule-guest-selected').textContent = guest?.name || 'No guest';
        node('schedule-vip-type').disabled = !guest;
    }
    async function open(s, action) {
        if (opening) return;
        opening = true;
        try {
            // Re-read both roster and server game date when opening a form.
            const [rosterRes, scheduleRes] = await Promise.all([fetch('/api/members'), fetch('/api/train-schedule')]);
            if (!rosterRes.ok || !scheduleRes.ok) throw new Error('Could not load current members and schedule.');
            members = (await rosterRes.json()).filter(m => m.rank !== 'EX');
            const data = await scheduleRes.json(); today = data.game_date;
            mode = action; target = s;
            conductor = s?.conductor_id ? { id: s.conductor_id, name: s.conductor_name } : null;
            guest = s?.vip_id ? { id: s.vip_id, name: s.vip_name } : null;
            const actual = mode === 'complete';
            node('schedule-modal-title').textContent = actual ? 'Complete train — actual result' : s ? 'Edit train plan' : 'Plan train';
            node('schedule-planned').replaceChildren();
            if (actual) {
                node('schedule-planned').append('Planned conductor: ', nameNode(s.conductor_name),
                    '. Planned guest: ', nameNode(s.vip_name || 'None'), '. The original plan will be kept.');
            }
            node('schedule-conductor-label').textContent = actual ? 'Actual conductor' : 'Planned conductor';
            node('schedule-guest-label').textContent = actual ? 'Actual guest (optional)' : 'Planned guest (optional)';
            node('schedule-notes-label').textContent = actual ? 'Train-log notes (actual result)' : 'Plan notes';
            node('schedule-date').value = s?.date || today; node('schedule-date').disabled = actual;
            node('schedule-date').min = actual ? '' : today;
            node('schedule-type').value = s?.train_type || 'FREE'; node('schedule-type').disabled = actual;
            node('schedule-vip-type').value = s?.vip_type || 'SPECIAL_GUEST';
            node('schedule-notes').value = s?.notes || '';
            node('schedule-save').textContent = actual ? 'Complete and log train' : 'Save plan';
            node('schedule-modal-status').textContent = '';
            pickers.forEach(p => p.destroy());
            pickers = ['conductor', 'guest'].map(role => {
                const picker = createMemberPicker({
                    getCandidates: () => members,
                    placeholder: 'Search ' + (actual ? 'actual ' : 'planned ') + role + '…',
                    onPick: m => { if (role === 'conductor') conductor = m; else guest = m; showSelections(); },
                });
                picker.input.setAttribute('aria-labelledby', 'schedule-' + (role === 'guest' ? 'guest' : 'conductor') + '-label');
                node('schedule-' + role + '-picker').replaceChildren(picker.el);
                return picker;
            });
            showSelections();
            const modal = node('schedule-modal'); modal.style.display = 'flex'; trapFocus(modal);
            if (actual) pickers[0].input.focus(); else node('schedule-date').focus();
        } catch (err) { console.error('openTrainSchedule:', err); showToast(err.message, 'error'); }
        finally { opening = false; }
    }
    function close() {
        const modal = node('schedule-modal'); releaseFocus(modal); modal.style.display = '';
        pickers.forEach(p => p.destroy()); pickers = [];
    }
    async function submit(e) {
        e.preventDefault();
        if (!conductor) { node('schedule-modal-status').textContent = 'Choose a conductor.'; return; }
        const body = { conductor_id: conductor.id, vip_id: guest?.id || null,
            vip_type: guest ? node('schedule-vip-type').value : null, notes: node('schedule-notes').value.trim() };
        if (mode !== 'complete') { body.date = node('schedule-date').value; body.train_type = node('schedule-type').value; }
        const url = '/api/train-schedule' + (target ? '/' + target.id : '') + (mode === 'complete' ? '/complete' : '');
        const save = node('schedule-save'); if (save.disabled) return; save.disabled = true;
        try {
            const res = await fetch(url, { method: mode === 'edit' ? 'PUT' : 'POST', headers: {'Content-Type':'application/json'}, body:JSON.stringify(body) });
            if (!res.ok) { node('schedule-modal-status').textContent = (await res.text()).trim() || 'Could not save this train.'; return; }
            const result = await res.json();
            close();
            if (mode === 'complete') {
                showToast(result.limit_warning ? 'Train logged. The daily train limit was exceeded.' : 'Train completed and logged.', result.limit_warning ? 'warning' : 'success');
                if (config.canViewTrain === 'true') loadTrainLogs();
            } else { showToast('Train plan saved.'); }
            await load();
        } catch (err) { console.error('saveTrainSchedule:', err); node('schedule-modal-status').textContent = 'Network error. Refresh the schedule before retrying.'; }
        finally { save.disabled = false; }
    }
    async function cancel(s) {
        if (!await showConfirm('Cancel the ' + s.date + ' ' + s.train_type + ' train planned for ' + s.conductor_name + '? The plan will be kept as cancelled.', 'Cancel train')) return;
        try {
            const res = await fetch('/api/train-schedule/' + s.id + '/cancel', {method:'POST'});
            if (!res.ok) { showToast((await res.text()).trim() || 'Could not cancel the plan.', 'error'); return; }
            showToast('Train plan cancelled.'); await load();
        } catch (err) { console.error('cancelTrainSchedule:', err); showToast('Network error. Refresh the schedule before retrying.', 'error'); }
    }
    document.addEventListener('DOMContentLoaded', () => {
        if (!canView) return;
        QuickSearch.attach({input:'schedule-search',container:'schedule-container',rows:'tbody tr',emptyText:'No plans match your search.'});
        node('schedule-refresh').addEventListener('click', load);
        if (!canManage) return;
        node('schedule-new').addEventListener('click', () => open(null, 'create'));
        node('schedule-close').addEventListener('click', close);
        node('schedule-modal').addEventListener('keydown', e => { if (e.key === 'Escape') close(); });
        node('schedule-modal').addEventListener('click', e => { if (e.target === node('schedule-modal')) close(); });
        node('schedule-form').addEventListener('submit', submit);
        node('schedule-clear-guest').addEventListener('click', () => { guest = null; showSelections(); });
    });
    return {load};
})();
