// job-progress.js — shared UI for server-side bulk jobs (see jobs.go).
//
// Replaces the four near-identical browser loops that used to drive these sweeps
// one fetch at a time with the tab held open. The run now lives on the server;
// this only watches it. That is the whole point: an officer can start a sweep,
// navigate away, come back — or open the page on their phone — and still see it.
//
// Because the run is server-side, the page ATTACHES to whatever is already
// running on load rather than owning the run. Reloading mid-sweep is harmless.

(function () {
    'use strict';

    const POLL_MS = 2000;
    const STATES = ['queued', 'active', 'done', 'skip', 'err'];

    function el(tag, className, text) {
        const n = document.createElement(tag);
        if (className) n.className = className;
        if (text != null) n.textContent = text;
        return n;
    }

    /**
     * attach(opts) wires a start button + progress panel to one job kind.
     *
     *   kind        job kind string (must match a registerJobKind in Go)
     *   startBtn    element that begins a run
     *   container   element the progress rows render into
     *   statusEl    optional element for the "Fetching i of N…" line
     *   cancelBtn   optional element to cancel a running job
     *   confirm     optional message shown via showConfirm before starting
     *   busyEls     elements disabled while a run is in progress
     *   onDone      called once when a run finishes (reload your data here)
     *   summarize   (counters, job) => string for the completion toast
     */
    function attach(opts) {
        const { kind, startBtn, container, statusEl, cancelBtn } = opts;
        if (!startBtn || !container) return null;

        let timer = null;
        let lastJobId = null;
        let lastItemCount = -1;
        let watching = false;
        const rows = new Map();

        const setStatus = m => { if (statusEl) statusEl.textContent = m || ''; };
        const busy = state => {
            startBtn.disabled = state;
            (opts.busyEls || []).forEach(e => { if (e) e.disabled = state; });
            if (cancelBtn) cancelBtn.style.display = state ? '' : 'none';
        };

        function rebuild(job) {
            container.replaceChildren();
            rows.clear();
            job.items.forEach(it => {
                const name = el('span', 'job-prog-name', it.label);
                const status = el('span', 'job-prog-status', '');
                const row = el('div', 'job-prog-row');
                row.append(name, status);
                rows.set(it.seq, { row, status });
                container.appendChild(row);
            });
            lastItemCount = job.items.length;
        }

        // Updates in place rather than re-rendering: a 100-row list rebuilt every
        // two seconds throws away the user's scroll position mid-run.
        function paint(job) {
            if (job.id !== lastJobId || job.items.length !== lastItemCount) {
                lastJobId = job.id;
                rebuild(job);
            }
            job.items.forEach(it => {
                const entry = rows.get(it.seq);
                if (!entry) return;
                const cls = STATES.includes(it.state) ? it.state : 'queued';
                const wanted = 'job-prog-row ' + cls;
                if (entry.row.className !== wanted) entry.row.className = wanted;
                const label = it.detail || (it.state === 'active' ? 'fetching…' : it.state === 'queued' ? 'queued' : '');
                if (entry.status.textContent !== label) entry.status.textContent = label;
            });
            container.style.display = 'block';
        }

        function finishToast(job) {
            let counters = {};
            try { counters = JSON.parse(job.counters || '{}'); } catch (e) { /* opaque blob */ }
            if (job.status === 'failed') {
                showToast(job.error || 'The sync failed.', 'error');
            } else if (job.status === 'cancelled') {
                showToast(`Cancelled after ${job.processed} of ${job.total}.`, 'info');
            } else if (job.status === 'interrupted') {
                showToast(`Interrupted after ${job.processed} of ${job.total}. Run it again to resume.`, 'info');
            } else if (job.total === 0) {
                showToast('Nothing to sync — everything is already up to date.', 'info');
            } else if (opts.summarize) {
                showToast(opts.summarize(counters, job));
            } else {
                showToast(`Finished ${job.processed} of ${job.total}.`);
            }
        }

        async function poll(announce) {
            let job;
            try {
                const res = await fetch('/api/jobs/current?kind=' + encodeURIComponent(kind));
                if (!res.ok) throw new Error(await res.text());
                job = (await res.json()).job;
            } catch (e) {
                stop();
                return;
            }
            if (!job) { stop(); return; }

            if (job.status === 'running') {
                paint(job);
                busy(true);
                watching = true;
                setStatus(`${job.trigger === 'scheduled' ? 'Scheduled sync' : 'Fetching'} ${job.processed} of ${job.total}…`);
                return;
            }

            // Finished. Paint the final state so the list reflects the outcome.
            paint(job);
            setStatus('');
            busy(false);
            const wasWatching = watching;
            stop();
            if (announce || wasWatching) {
                finishToast(job);
                if (opts.onDone) opts.onDone(job);
            }
        }

        function start() {
            if (timer) return;
            timer = setInterval(() => poll(true), POLL_MS);
        }
        function stop() {
            watching = false;
            if (timer) { clearInterval(timer); timer = null; }
        }

        startBtn.addEventListener('click', async () => {
            if (opts.confirm && !await showConfirm(opts.confirm, 'Start')) return;
            busy(true);
            try {
                const res = await fetch('/api/jobs/start', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ kind })
                });
                if (!res.ok) throw new Error((await res.text()) || 'Could not start');
                container.replaceChildren();
                rows.clear();
                lastItemCount = -1;
                watching = true;
                await poll(false);
                start();
            } catch (e) {
                busy(false);
                // A 409 carries the running kind and elapsed time from the server.
                showToast((e.message || 'Could not start the sync.').trim(), 'error');
            }
        });

        if (cancelBtn) {
            cancelBtn.style.display = 'none';
            cancelBtn.addEventListener('click', async () => {
                if (lastJobId == null) return;
                if (!await showConfirm('Stop this sync? Everything already synced is kept.', 'Stop')) return;
                try {
                    await fetch('/api/jobs/cancel', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ id: lastJobId })
                    });
                    setStatus('Stopping after the current item…');
                } catch (e) { showToast('Could not cancel.', 'error'); }
            });
        }

        // Adopt a run already in progress (started elsewhere, or before a reload).
        // Silent: no toast for a run this page never started and may have missed
        // the end of.
        (async () => {
            try {
                const res = await fetch('/api/jobs/current?kind=' + encodeURIComponent(kind));
                if (!res.ok) return;
                const job = (await res.json()).job;
                if (job && job.status === 'running') {
                    lastJobId = job.id;
                    paint(job);
                    busy(true);
                    watching = true;
                    start();
                }
            } catch (e) { /* nothing running, or not permitted — leave the panel closed */ }
        })();

        return { poll, stop };
    }

    window.JobProgress = { attach };
})();
