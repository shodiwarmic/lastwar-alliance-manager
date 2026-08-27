// Inline translation for member-written prose.
//
// The app has no i18n layer: the UI is English and non-English speakers use the
// browser's own page translation. That is page-level and all-or-nothing, so it
// cannot help the opposite case -- an English reader meeting one Spanish
// shout-out inside an otherwise English page. `layout.html` declares
// `lang="en"` and the surrounding UI is English, so Chrome detects the page as
// English and never offers the bar, however foreign that one card is.
//
// This attaches a per-block Translate control to member-written prose, driven by
// Chrome/Edge's built-in on-device Translator + LanguageDetector APIs. Nothing
// leaves the reader's machine: no API key, no server round-trip, no cost.
//
// DESKTOP ONLY. The built-in AI APIs do not exist on mobile browsers and need a
// secure context, so on a phone -- or over plain-HTTP LAN access -- the control
// simply never renders. Absent, not broken. Making these blocks translatable on
// mobile needs a server-side fallback; see P3 in translation-handling-proposals.md.
//
// Never automatic and never page-wide: translating is always an explicit click on
// one block, and the original is always one click back.

(function () {
    'use strict';

    const HAS_API = 'Translator' in self && 'LanguageDetector' in self;

    // The reader's language, base subtag only ('en-GB' -> 'en'). The translation
    // APIs key on the base tag, and regional variants would fragment the cache.
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

    let detectorPromise = null;
    let detectorChecked = false;

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

    // Create the shared detector only when it needs no download. Creating a
    // downloadable model requires user activation, which a render pass does not
    // have -- blocks rendered before then fall back to resolving on first click,
    // which does carry activation.
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
     * Attach a Translate control to one block of member-written prose.
     *
     * @param {HTMLElement} el   Element whose text is the prose. Its children are
     *                           replaced: the text moves into a span so the
     *                           control survives swapping the text back and forth.
     * @param {string} sourceText The prose, from the DATA rather than the DOM.
     */
    function attach(el, sourceText) {
        if (!HAS_API || !el) return;

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

        // Decide whether this block needs a control at all. When the detector is
        // ready we can be precise and stay silent on prose already in the
        // reader's language; when it is not, we show the control and settle the
        // question on click.
        (async () => {
            const detector = await sharedDetector();
            if (detector) {
                sourceLang = await detectLang(text, detector);
                if (sourceLang === TARGET) return;      // nothing to offer
                if (sourceLang) {
                    if ((await pairAvailability(sourceLang)) === 'unavailable') return;
                    btn.title = 'Translate from ' + langName(sourceLang);
                }
            }
            btn.hidden = false;
        })();

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
                span.textContent = translated;
                span.lang = TARGET;
                setLabel(btn, 'Show original');
                showingTranslation = true;
                return;
            }

            busy = true;
            btn.disabled = true;
            status.hidden = true;
            setLabel(btn, 'Translating…');

            // Giving up on the promise does not by itself stop the work behind
            // it; the session would stay half-created and hold the model. Both
            // engines take a signal, so a stall can actually release it.
            const aborter = new AbortController();

            try {
                if (!sourceLang) {
                    // This click is user activation, so a model download is
                    // permitted now even though it was not at render time.
                    sourceLang = await detectLang(text, await LanguageDetector.create());
                }
                if (!sourceLang) {
                    note('Could not identify the language.');
                    setLabel(btn, 'Translate');
                    return;
                }
                if (sourceLang === TARGET) {
                    note('Already in ' + langName(TARGET) + '.');
                    btn.hidden = true;
                    return;
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
                            if (guard) guard.bump();
                            setLabel(btn, 'Downloading ' + progressPct(e) + '%');
                        });
                    },
                });
                guard = withStallTimeout(creating, STALL_MS);
                const translator = await guard.result;

                setLabel(btn, 'Translating…');
                translated = await withStallTimeout(
                    translator.translate(text, { signal: aborter.signal }), STALL_MS).result;

                // The model stays in memory until the session is released, and
                // the result is cached above, so this translator has no further use.
                try { translator.destroy(); } catch { /* not in every build */ }
                span.textContent = translated;
                span.lang = TARGET;
                setLabel(btn, 'Show original');
                showingTranslation = true;
            } catch (err) {
                // A stall means the engine accepted the request and then went
                // quiet -- most often a first-run model download that never
                // started. The pack may still arrive, so this is worth retrying
                // rather than reporting as a permanent failure.
                const stalled = err && err.message === 'stalled';
                if (stalled) aborter.abort();
                note(stalled
                    ? 'Still preparing this language. Try again in a moment.'
                    : 'Translation unavailable.');
                setLabel(btn, 'Translate');
                console.warn('[translate] ' + (sourceLang || '?') + '→' + TARGET + ':', err);
            } finally {
                busy = false;
                btn.disabled = false;
            }
        });
    }

    window.TranslateBlock = { attach, supported: HAS_API };
})();
