// donation-marks.js — how a donation day and a week's total read, for every page that shows
// them (Rankings, Accountability, the member profile, Reports). An opt-in module: a page
// links donation-marks.css first and loads this before its own script.
//
// The server derives every status (donations.go): a day is exact (two consecutive readings
// pin it), so far (only that day's own "today" reading), or blank; a total is final (a
// Sunday reading once the week is over), so far, or blank. Exact reads plain, so far muted,
// blank as a dash; each cell exports its raw amount, blank when blank.
'use strict';

const DonationMarks = (() => {
    function amount(n) {
        return Number(n).toLocaleString('en-US');
    }

    function dayCell(day) {
        const td = document.createElement('td');
        if (!day || day.status === 'blank') {
            td.className = 'don-blank';
            td.textContent = '—';
            td.title = day && day.note ? `Not read (${day.note})` : 'Not read';
            td.dataset.exportValue = '';
            return td;
        }
        td.textContent = amount(day.amount);
        td.dataset.exportValue = String(day.amount);
        if (day.entered) {
            td.className = 'don-entered';
            td.title = 'Entered by hand';
        } else if (day.status === 'so_far') {
            td.className = 'don-so-far';
            td.title = day.note
                ? `So far — this day's readings were ${day.note}`
                : 'So far — the next day\'s reading pins it exactly';
        }
        return td;
    }

    // j carries below_weekly / not_read_to_end when the week was judged.
    function totalCell(total, j) {
        const td = document.createElement('td');
        if (!total || total.status === 'blank') {
            td.className = 'don-blank';
            td.textContent = '—';
            td.title = 'No week reading';
            td.dataset.exportValue = '';
            return td;
        }
        td.textContent = amount(total.amount);
        td.dataset.exportValue = String(total.amount);
        if (j && j.below_weekly) {
            td.className = 'don-below';
            td.title = 'Below the weekly minimum';
        } else if (j && j.not_read_to_end) {
            td.className = 'don-so-far';
            td.title = 'Not read to the end — no Sunday reading, so not judged';
        } else if (total.status === 'so_far') {
            td.className = 'don-so-far';
            td.title = 'So far — final once Sunday is read and the week is over';
        }
        return td;
    }

    function legend() {
        const p = document.createElement('p');
        p.className = 'don-legend';
        const mark = (cls, text) => Object.assign(document.createElement('span'), { className: cls, textContent: text });
        p.append(
            'Days: plain = exact (readings on two days in a row pin it); ',
            mark('don-entered', 'dotted'), ' = entered by hand; ',
            mark('don-so-far', 'muted'), ' = so far (that day\'s own reading only); ',
            mark('don-blank', '—'), ' = not read. ',
            'Sunday is never exact — the week restarts on Monday — so its last reading counts once the week is over. ',
            'Week: the latest weekly reading; final from a Sunday reading; ',
            mark('don-below', 'red'), ' = below the weekly minimum.'
        );
        return p;
    }

    return { amount, dayCell, totalCell, legend };
})();
