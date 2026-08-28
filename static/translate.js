// Inline translation for member-written prose.
//
// The app has no i18n layer: the UI is English and non-English speakers use the
// browser's own page translation. That is page-level and all-or-nothing, so it
// cannot help the opposite case -- an English reader meeting one Spanish
// shout-out inside an otherwise English page. `layout.html` declares
// `lang="en"` and the surrounding UI is English, so Chrome detects the page as
// English and never offers the bar, however foreign that one card is.
//
// Resolution order, in this order and for these reasons:
//
//   1. SHARED SERVER CACHE -- keyed on a hash of the text plus the target
//      language, so it needs no language detection at all. That is what makes
//      it viable as the first tier.
//   2. ON-DEVICE, ONLY IF INSTANTLY READY -- the browser's built-in Translator,
//      used solely when availability() says 'available'. Free and private, with
//      no stall risk. Note that Chrome masks pack status for anti-fingerprinting
//      and reports 'downloadable' regardless of what is cached, so on Chrome
//      this tier rarely fires; it is honest rather than load-bearing.
//   3. SERVER BACKEND -- Cloud Translation, which auto-detects the source
//      language, so this tier needs no client detection either. That is what
//      makes it work on a phone, where the built-in APIs do not exist at all.
//
// On-device results are NEVER written back to the shared cache: that store is
// read by every user, and letting a client put arbitrary text into it would be
// a way to put words in another member's mouth. The cache is server-authored.
//
// With no server backend configured (`ondevice` mode) this file behaves exactly
// as it did when it shipped: browser-only, desktop-only, with a stall guard.

(function () {
    'use strict';

    const HAS_API = 'Translator' in self && 'LanguageDetector' in self;

    // Whether a SERVER backend is configured. `ondevice` is deliberately not
    // called `off` -- the browser path keeps working in every mode.
    const cfgEl = document.getElementById('layout-config');
    const SERVER_MODE = !!cfgEl && cfgEl.dataset.translationMode
        && cfgEl.dataset.translationMode !== 'ondevice';

    // The reader's language, base subtag only ('en-GB' -> 'en'). The engines key
    // on the base tag, and regional variants would fragment the cache.
    const TARGET = (navigator.language || 'en').split('-')[0].toLowerCase();

    // Below this, detection is noise rather than signal -- "ok", "+5", "n/a".
    const MIN_CHARS = 12;
    // Detections we are not reasonably sure of are treated as no detection: a
    // wrong guess either hides a control the reader needs or offers to translate
    // text already in their language.
    const MIN_CONFIDENCE = 0.5;

    // Give up only when nothing has happened for this long. A first-run language
    // pack can take minutes and Translator.create() stays pending for the whole
    // download, so a total-duration cap would kill working downloads. What is
    // never acceptable is a control stuck on "Translating…" forever with no way
    // back, which is what an unguarded await gives you.
    const STALL_MS = 45000;

    // Language pairs that failed to become usable this session. A pack that will
    // not download fails for every block in that language, so one 45-second wait
    // is informative and ten in a row is just punishment.
    const failedPairs = new Set();
    const pairKey = src => src + '>' + TARGET;

    let detectorPromise = null;
    let detectorChecked = false;

    let displayNames = null;
    try {
        displayNames = new Intl.DisplayNames([TARGET], { type: 'language' });
    } catch { /* Intl.DisplayNames is optional sugar for the tooltip */ }

    function langName(code) {
        try {
            return (displayNames && displayNames.of(code)) || code;
        } catch {
            return code;
        }
    }

    // Rejects when `bump()` has not been called for `stallMs`. Callers bump on
    // every sign of life (download progress), so a slow-but-moving download is
    // left alone and only a genuinely wedged one is abandoned.
    function withStallTimeout(promise, stallMs) {
        let rejectGuard;
        let timer;
        const guard = new Promise((_, rej) => { rejectGuard = rej; });
        const bump = () => {
            clearTimeout(timer);
            timer = setTimeout(() => rejectGuard(new Error('stalled')), stallMs);
        };
        bump();
        return { bump, result: Promise.race([promise, guard]).finally(() => clearTimeout(timer)) };
    }

    // Create the shared detector only when it needs no download. Creating a
    // downloadable model requires user activation, which a render pass does not
    // have -- blocks rendered before then settle their language on first click.
    async function sharedDetector() {
        if (detectorChecked) return detectorPromise;
        detectorChecked = true;
        try {
            if ((await LanguageDetector.availability()) === 'available') {
                detectorPromise = LanguageDetector.create();
            }
        } catch {
            detectorPromise = null;
        }
        return detectorPromise;
    }

    async function detectLang(text, detector) {
        try {
            const results = await detector.detect(text);
            const top = results && results[0];
            if (!top || top.confidence < MIN_CONFIDENCE) return null;
            return String(top.detectedLanguage).split('-')[0].toLowerCase();
        } catch {
            return null;
        }
    }

    async function pairAvailability(source) {
        try {
            return await Translator.availability({
                sourceLanguage: source,
                targetLanguage: TARGET,
            });
        } catch {
            return 'unavailable';
        }
    }

    // The two engines disagree on the shape of downloadprogress. Chrome reports
    // `loaded` as a 0-1 fraction; Edge reports `loaded`/`total` as byte counts.
    // Multiplying Edge's byte count by 100 renders "Downloading 4823700%".
    function progressPct(e) {
        const loaded = e.loaded || 0;
        if (e.total) return Math.round((loaded / e.total) * 100);
        return Math.round(loaded * 100);
    }

    function setLabel(btn, text) {
        btn.replaceChildren(svgIcon('language', 12), document.createTextNode(' ' + text));
    }

    /**
     * Ask the server. `cacheOnly` asks for tier 1 alone: answer from the cache
     * or report a miss, never spend quota.
     *
     * The source text is POSTed rather than a hash of it, because hashing in the
     * browser needs crypto.subtle -- which exists only in a secure context. This
     * app is routinely reached over plain HTTP on a LAN, which is exactly the
     * deployment a server backend exists to serve.
     */
    async function serverTranslate(text, cacheOnly) {
        const res = await fetch('/api/translate', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ text, target: TARGET, cache_only: !!cacheOnly }),
        });
        if (res.status === 204) return null;            // cache miss, by design
        if (!res.ok) {
            const detail = (await res.text()).trim();
            throw new Error(detail || 'Translation unavailable.');
        }
        const data = await res.json();
        return {
            text: data.translated_text || '',
            alreadyTarget: !!data.already_target,
        };
    }

    /**
     * Attach a Translate control to one block of member-written prose.
     *
     * @param {HTMLElement} el   Element whose text is the prose. Its children are
     *                           replaced: the text moves into a span so the
     *                           control survives swapping the text back and forth.
     * @param {string} sourceText The prose, from the DATA rather than the DOM.
     */
    function attach(el, sourceText) {
        if ((!HAS_API && !SERVER_MODE) || !el) return;

        const text = String(sourceText == null ? el.textContent || '' : sourceText).trim();
        if (text.length < MIN_CHARS) return;

        const span = document.createElement('span');
        span.className = 'tl-text';
        span.textContent = text;
        el.replaceChildren(span);

        // Anything reading this block as a value -- a CSV export today, anything
        // else tomorrow -- must see the original, never the translation. Same
        // rule as the battle mail and the table exports (CLAUDE.md -> "Browser
        // translation"); this feature would otherwise reintroduce exactly the
        // class of bug that rule exists to prevent.
        el.dataset.exportText = text;

        const btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'tl-btn';
        btn.hidden = true;
        setLabel(btn, 'Translate');

        const status = document.createElement('span');
        status.className = 'tl-status';
        status.hidden = true;

        el.append(btn, status);

        let sourceLang = null;
        let translated = null;
        let showingTranslation = false;
        let busy = false;

        function note(message) {
            status.textContent = message;
            status.hidden = false;
        }

        function showTranslation(value) {
            span.textContent = value;
            span.lang = TARGET;
            setLabel(btn, 'Show original');
            showingTranslation = true;
        }

        // Decide whether this block needs a control at all.
        //
        // Where the detector is ready we can be precise and stay silent on prose
        // already in the reader's language. Where it is not -- notably on mobile,
        // which has no LanguageDetector and is exactly where a server backend
        // matters most -- we show the control and let the server settle the
        // language. A same-language answer is cached, so that noise corrects
        // itself per note rather than on every view.
        (async () => {
            const detector = HAS_API ? await sharedDetector() : null;
            if (detector) {
                sourceLang = await detectLang(text, detector);
                if (sourceLang === TARGET) return;      // nothing to offer
                if (sourceLang) btn.title = 'Translate from ' + langName(sourceLang);
            }
            if (!SERVER_MODE && sourceLang) {
                // With no server fallback, a pair the engine cannot serve means
                // a control that could only ever fail.
                if (failedPairs.has(pairKey(sourceLang))) return;
                if ((await pairAvailability(sourceLang)) === 'unavailable') return;
            }
            btn.hidden = false;
        })();

        // The on-device attempt, with its own download wait, stall guard and
        // failure diagnostics. Returns a result, or null having reported why.
        async function onDeviceTranslate() {
            // Giving up on the promise does not by itself stop the work behind
            // it; the session would stay half-created and hold the model. Both
            // engines take a signal, so a stall can actually release it.
            const aborter = new AbortController();
            const startedAt = performance.now();
            let progressEvents = 0;

            try {
                if (!sourceLang) {
                    // This click is user activation, so a model download is
                    // permitted now even though it was not at render time.
                    sourceLang = await detectLang(text, await LanguageDetector.create());
                }
                if (!sourceLang) {
                    note('Could not identify the language.');
                    return null;
                }
                if (sourceLang === TARGET) {
                    return { text: '', alreadyTarget: true };
                }

                // Chrome reports every pair as "downloadable" regardless of what
                // is actually cached, so there is no way to know in advance
                // whether this call is instant or a multi-minute download. Say
                // "Preparing" until the answer arrives rather than "Translating",
                // which implies work that has not started yet.
                setLabel(btn, 'Preparing…');

                let guard;
                const creating = Translator.create({
                    sourceLanguage: sourceLang,
                    targetLanguage: TARGET,
                    signal: aborter.signal,
                    monitor(m) {
                        m.addEventListener('downloadprogress', e => {
                            progressEvents++;
                            if (guard) guard.bump();
                            setLabel(btn, 'Downloading ' + progressPct(e) + '%');
                        });
                    },
                });
                guard = withStallTimeout(creating, STALL_MS);
                const translator = await guard.result;

                setLabel(btn, 'Translating…');
                const out = await withStallTimeout(
                    translator.translate(text, { signal: aborter.signal }), STALL_MS).result;

                // The model stays in memory until the session is released, and
                // the result is held above, so this translator has no further use.
                try { translator.destroy(); } catch { /* not in every build */ }
                return { text: out, alreadyTarget: false };
            } catch (err) {
                const stalled = err && err.message === 'stalled';
                if (stalled) {
                    aborter.abort();
                    if (sourceLang) failedPairs.add(pairKey(sourceLang));
                }

                // "stalled" on its own does not distinguish a pair the engine
                // cannot supply from one it accepted and never began downloading.
                // Report enough to tell them apart without anyone having to run a
                // console snippet by hand.
                let pairState = 'n/a';
                let detectorState = 'n/a';
                try {
                    if (sourceLang) {
                        pairState = await Translator.availability({
                            sourceLanguage: sourceLang, targetLanguage: TARGET,
                        });
                    }
                } catch (e) { pairState = 'threw: ' + e.name; }
                try {
                    detectorState = await LanguageDetector.availability();
                } catch (e) { detectorState = 'threw: ' + e.name; }

                console.warn('[translate] on-device FAILED — copy this whole object:', {
                    pair: (sourceLang || '?') + '->' + TARGET,
                    error: err && (err.name + ': ' + err.message),
                    pairAvailability: pairState,
                    detectorAvailability: detectorState,
                    progressEvents,
                    elapsedMs: Math.round(performance.now() - startedAt),
                    secureContext: window.isSecureContext,
                    serverBackend: SERVER_MODE,
                    ua: navigator.userAgentData
                        ? JSON.stringify(navigator.userAgentData.brands)
                        : navigator.userAgent,
                });

                // With a server backend the caller falls through to it, so this
                // is not the end of the road and must not claim to be.
                if (!SERVER_MODE) {
                    note(stalled
                        ? 'Still preparing this language. Try again in a moment.'
                        : 'Translation unavailable.');
                }
                return null;
            }
        }

        // Walk the tiers. Returns a result, or null having reported why.
        async function resolve() {
            // Is the browser able to answer instantly? Only then is the extra
            // cache round trip worth making before asking the server, and only
            // then can on-device run without any stall risk.
            const quickLocal = HAS_API && sourceLang && sourceLang !== TARGET
                && !failedPairs.has(pairKey(sourceLang))
                && (await pairAvailability(sourceLang)) === 'available';

            if (quickLocal) {
                if (SERVER_MODE) {
                    try {
                        const hit = await serverTranslate(text, true);   // tier 1
                        if (hit) return hit;
                    } catch {
                        // A cache probe failing is not worth reporting; the
                        // remaining tiers can still answer.
                    }
                }
                const local = await onDeviceTranslate();                 // tier 2
                if (local) return local;
                if (!SERVER_MODE) return null;   // already reported
            }

            if (SERVER_MODE) {                                           // tier 3
                try {
                    return await serverTranslate(text, false);
                } catch (err) {
                    note(err.message || 'Translation unavailable.');
                    return null;
                }
            }

            return await onDeviceTranslate();
        }

        btn.addEventListener('click', async () => {
            if (busy) return;

            if (showingTranslation) {
                span.textContent = text;
                span.removeAttribute('lang');
                setLabel(btn, 'Translate');
                showingTranslation = false;
                return;
            }

            if (translated !== null) {
                showTranslation(translated);
                return;
            }

            busy = true;
            btn.disabled = true;
            status.hidden = true;
            setLabel(btn, 'Translating…');

            try {
                const result = await resolve();
                if (!result) {
                    setLabel(btn, 'Translate');
                    return;
                }
                if (result.alreadyTarget) {
                    note('Already in ' + langName(TARGET) + '.');
                    btn.hidden = true;
                    return;
                }
                translated = result.text;
                showTranslation(translated);
            } finally {
                busy = false;
                btn.disabled = false;
            }
        });
    }

    window.TranslateBlock = { attach, supported: HAS_API || SERVER_MODE };
})();
