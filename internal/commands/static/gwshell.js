(function () {
  var shell = document.querySelector('.gw-shell');
  if (!shell) return;
  var KEY = 'gwrail';
  var saved = localStorage.getItem(KEY);
  if (saved) shell.setAttribute('data-rail', saved);

  var SET_DEFAULT = { gwrail: 'expanded', gwfeedinterval: '5', gwtz: 'local', gwtc: 'on', gwday: 'on' };
  var SET_ALLOWED = { gwrail: ['collapsed', 'expanded'], gwfeedinterval: ['0', '5', '15', '30'], gwtz: ['local', 'utc'], gwtc: ['on', 'off'], gwday: ['on', 'off'] };

  document.body.dataset.tc = (localStorage.getItem('gwtc') === 'off') ? 'off' : 'on';

  // Apply the feed auto-refresh interval before htmx processes the DOM. gwshell.js
  // is `defer` and htmx processes on DOMContentLoaded, so this mutation lands first.
  var feed = document.getElementById('feed');
  if (feed) {
    var iv = localStorage.getItem('gwfeedinterval');
    if (SET_ALLOWED.gwfeedinterval.indexOf(iv) === -1) iv = SET_DEFAULT.gwfeedinterval;
    feed.setAttribute('hx-trigger', iv === '0' ? 'load' : 'load, every ' + iv + 's');
  }


  // Timezone display: convert every <time class="gw-ts"> from its UTC datetime to
  // the viewer's local zone (default), or restore server/UTC text. Runs on load
  // and after each htmx swap (feed / #stats-results rows are replaced on poll).
  function gwApplyTz(root) {
    var mode = localStorage.getItem('gwtz');
    if (SET_ALLOWED.gwtz.indexOf(mode) === -1) mode = SET_DEFAULT.gwtz;
    // Compact format on narrow viewports: time-only (day-separator carries the
    // date, so the column collapses without losing the date context).
    var compact = window.innerWidth < 600;
    (root || document).querySelectorAll('time.gw-ts').forEach(function (el) {
      if (!el.dataset.utc) el.dataset.utc = el.textContent; // remember the server text
      if (mode === 'utc' && !compact) { el.textContent = el.dataset.utc; return; }
      var d = new Date(el.getAttribute('datetime'));
      if (!isNaN(d.getTime())) {
        var opts = compact
          ? { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }
          : { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false };
        el.textContent = d.toLocaleString(undefined, opts);
      }
    });
  }

  // /feed: client-side severity-bucket filter (BLOCK/WARN/INFO/clean chips)
  // composed (AND) with the text-search input; updates the "N shown" count.
  function gwFeedFilter() {
    var feed = document.getElementById('feed');
    if (!feed) return;
    var active = {};
    document.querySelectorAll('.gw-feedchip').forEach(function (c) {
      active[c.getAttribute('data-feedsev')] = c.getAttribute('aria-pressed') !== 'false';
    });
    var box = document.getElementById('feed-search');
    var q = (box ? box.value : '').trim().toLowerCase();
    var shown = 0;
    feed.querySelectorAll('tr[data-feedsev]').forEach(function (tr) {
      var b = tr.getAttribute('data-feedsev');
      var sevOK = active[b] !== false;
      var textOK = q === '' || tr.textContent.toLowerCase().indexOf(q) !== -1;
      var show = sevOK && textOK;
      tr.style.display = show ? '' : 'none';
      if (show) shown++;
    });
    var cnt = document.getElementById('feed-count');
    if (cnt) cnt.textContent = q ? (shown + ' shown') : '';
  }

  // Day separators: insert a date row in the feed wherever the DISPLAYED day
  // changes (follows the gwtz setting - local or UTC - reading the same datetime +
  // mode). Client-side so it re-runs with the conversion (load, htmx swap, setting
  // change); the feed still renders fine without JS, just ungrouped.
  function pad(n) { return (n < 10 ? '0' : '') + n; }
  function gwDaySeparators() {
    document.querySelectorAll('#feed tr.gw-daysep').forEach(function (el) { el.remove(); });
    if (localStorage.getItem('gwday') === 'off') return;
    var utc = localStorage.getItem('gwtz') === 'utc';
    document.querySelectorAll('#feed table.fr tbody').forEach(function (tbody) {
      var prevKey = null;
      Array.prototype.slice.call(tbody.children).forEach(function (tr) {
        if (tr.style.display === 'none') return;
        var t = tr.querySelector('time.gw-ts');
        if (!t) return;
        var d = new Date(t.getAttribute('datetime'));
        if (isNaN(d.getTime())) return;
        var y, mo, da, dayNum;
        if (utc) {
          y = d.getUTCFullYear(); mo = d.getUTCMonth() + 1; da = d.getUTCDate();
          dayNum = Math.floor(d.getTime() / 86400000);
        } else {
          y = d.getFullYear(); mo = d.getMonth() + 1; da = d.getDate();
          dayNum = Math.floor((d.getTime() - d.getTimezoneOffset() * 60000) / 86400000);
        }
        var key = y * 10000 + mo * 100 + da;
        if (prevKey === null || key !== prevKey) {
          var label = y + '-' + pad(mo) + '-' + pad(da) + (utc ? ' · UTC' : '');
          var sep = document.createElement('tr');
          sep.className = 'gw-daysep';
          var td = document.createElement('td');
          td.colSpan = 3;
          td.className = 'gw-dc-' + (((dayNum % 7) + 7) % 7);
          td.textContent = label;
          sep.appendChild(td);
          tbody.insertBefore(sep, tr);
        }
        prevKey = key;
      });
    });
  }

  // /feed: per-finding click-to-expand and per-ref click-to-expand. Both
  // share the same key set (row datetime + button text) so opened details
  // survive the 5s htmx swap. button.fnd reveals the finding's .dmsg via
  // CSS sibling selector; button.gw-ref reveals the row's .gw-rmsg messages
  // the same way. Resets on full reload.
  var gwOpen = new Set();
  function gwExpandKey(btn) {
    var tr = btn.closest('tr');
    var t = tr ? tr.querySelector('time.gw-ts') : null;
    var dt = t ? (t.getAttribute('datetime') || '') : '';
    return dt + '|' + btn.textContent;
  }
  function gwApplyExpand() {
    document.querySelectorAll('#feed button.fnd, #feed button.gw-ref').forEach(function (btn) {
      btn.setAttribute('aria-expanded', gwOpen.has(gwExpandKey(btn)) ? 'true' : 'false');
    });
  }
  document.body.addEventListener('click', function (e) {
    var btn = e.target.closest ? e.target.closest('button.fnd, button.gw-ref') : null;
    if (!btn || !btn.closest('#feed')) return;
    var k = gwExpandKey(btn);
    if (gwOpen.has(k)) { gwOpen.delete(k); btn.setAttribute('aria-expanded', 'false'); }
    else { gwOpen.add(k); btn.setAttribute('aria-expanded', 'true'); }
  });

  gwApplyTz(document);
  gwFeedFilter();
  gwApplyExpand();
  gwDaySeparators();
  gwReportFilter();
  document.body.addEventListener('htmx:afterSwap', function (e) {
    gwApplyTz(e.target); gwFeedFilter(); gwApplyExpand(); gwDaySeparators();
    // A freshly-run report replaces #report-out; clear any stale filter text so
    // the new rows aren't hidden by the previous query, then (re)apply.
    if (e.target && e.target.id === 'report-out') { var rb = document.getElementById('report-filter'); if (rb) rb.value = ''; }
    gwReportFilter();
  });
  // Surface htmx error responses. htmx's default responseHandling does NOT swap
  // 4xx/5xx responses, so without this a failed POST (duplicate repo name,
  // invalid field, credential save failure, …) is completely silent - the form
  // just appears to do nothing. Show the server's error text in a dismissible
  // banner so the gateway never fails quietly in the UI.
  function gwShowError(msg) {
    var bar = document.getElementById('gw-errbar');
    if (!bar) {
      bar = document.createElement('div');
      bar.id = 'gw-errbar';
      bar.style.cssText = 'position:fixed;top:0;left:0;right:0;z-index:99999;' +
        'padding:10px 16px;cursor:pointer;font:13px/1.45 -apple-system,Segoe UI,Roboto,sans-serif;' +
        'background:var(--gw-error-bg,#3a1d1d);color:var(--gw-error-text,#f8d4d4);' +
        'border-bottom:1px solid var(--gw-error-border,#c33)';
      bar.title = 'click to dismiss';
      bar.addEventListener('click', function () { bar.remove(); });
      document.body.appendChild(bar);
    }
    bar.textContent = msg;
    clearTimeout(bar._t);
    bar._t = setTimeout(function () { if (bar) bar.remove(); }, 8000);
  }
  document.body.addEventListener('htmx:responseError', function (e) {
    var xhr = e.detail && e.detail.xhr;
    var txt = (xhr && (xhr.responseText || '').trim()) || 'Request failed.';
    if (txt.length > 300) txt = txt.slice(0, 300) + '…';
    gwShowError(txt);
  });

  // Re-format timestamps on resize so the compact/full switch flips when the
  // window crosses the 600px breakpoint (mobile rotation, desktop split-screen).
  window.addEventListener('resize', function () { gwApplyTz(document); });

  // /frames page: live text filter + severity chips. Client-only; a frame li
  // shows iff its text matches the search AND its severity chip is active.
  // Subcategories with no visible frames + categories with no visible
  // subcategories collapse out of view. When a filter is active, matching
  // <details> auto-open so hits are visible without manual expand. Scoped to
  // #frames-catalog so it's inert on every other page.
  function gwFrameFilter() {
    var root = document.getElementById('frames-catalog');
    if (!root) return;
    var box = document.getElementById('frame-search');
    var q = (box ? box.value : '').trim().toLowerCase();
    var active = {};
    var anyOff = false;
    document.querySelectorAll('.gw-sevchip').forEach(function (c) {
      var on = c.getAttribute('aria-pressed') !== 'false';
      active[c.getAttribute('data-sev')] = on;
      if (!on) anyOff = true;
    });
    var filtering = q !== '' || anyOff;
    var shown = 0;
    root.querySelectorAll('li[data-sev]').forEach(function (li) {
      var sev = li.getAttribute('data-sev');
      var textOK = q === '' || li.textContent.toLowerCase().indexOf(q) !== -1;
      var sevOK = active[sev] !== false;
      var show = textOK && sevOK;
      li.style.display = show ? '' : 'none';
      if (show) shown++;
    });
    root.querySelectorAll('details.gw-sub').forEach(function (sub) {
      var anyVisible = false;
      sub.querySelectorAll('li[data-sev]').forEach(function (li) {
        if (li.style.display !== 'none') anyVisible = true;
      });
      // When no filter is active, keep empty sub-buckets visible so the
      // axis shape (e.g., Framework > Svelte/Astro/...) stays discoverable
      // even before any frame declares one. Only hide-on-empty during
      // active filtering, when irrelevant rows should collapse out of view.
      if (filtering) {
        sub.style.display = anyVisible ? '' : 'none';
        if (anyVisible) sub.open = true;
      } else {
        sub.style.display = '';
      }
    });
    root.querySelectorAll('details.gw-cat').forEach(function (cat) {
      var anyVisible = false;
      cat.querySelectorAll('details.gw-sub').forEach(function (sub) {
        if (sub.style.display !== 'none') anyVisible = true;
      });
      if (filtering) {
        cat.style.display = anyVisible ? '' : 'none';
        if (anyVisible) cat.open = true;
      } else {
        cat.style.display = '';
      }
    });
    var cnt = document.getElementById('frame-count');
    if (cnt) cnt.textContent = shown + ' shown';
  }

  var fsearch = document.getElementById('frame-search');
  if (fsearch) {
    fsearch.addEventListener('input', gwFrameFilter);
    document.querySelectorAll('.gw-sevchip').forEach(function (c) {
      c.addEventListener('click', function () {
        c.setAttribute('aria-pressed', c.getAttribute('aria-pressed') === 'false' ? 'true' : 'false');
        gwFrameFilter();
      });
    });
    gwFrameFilter();
  }

  document.querySelectorAll('.gw-feedchip').forEach(function (c) {
    c.addEventListener('click', function () {
      c.setAttribute('aria-pressed', c.getAttribute('aria-pressed') === 'false' ? 'true' : 'false');
      gwFeedFilter();
      gwDaySeparators();
    });
  });
  var fbox = document.getElementById('feed-search');
  if (fbox) {
    fbox.addEventListener('input', function () { gwFeedFilter(); gwDaySeparators(); });
  }

  // /events page: text search + repo dropdown + group chips. Same shape as
  // /feed's filter pass - client-only, scoped to #events-list.
  function gwEventsFilter() {
    var table = document.getElementById('events-list');
    if (!table) return;
    var box = document.getElementById('events-search');
    var q = (box ? box.value : '').trim().toLowerCase();
    var repoSel = document.querySelector('[data-events-repo]');
    var wantRepo = repoSel ? repoSel.value : '';
    var active = {};
    document.querySelectorAll('.gw-evchip').forEach(function (c) {
      active[c.getAttribute('data-evgroup')] = c.getAttribute('aria-pressed') !== 'false';
    });
    var shown = 0;
    table.querySelectorAll('tr[data-evgroup]').forEach(function (tr) {
      var g = tr.getAttribute('data-evgroup');
      var groupOK = active[g] !== false;
      var rowRepo = tr.getAttribute('data-repo') || '';
      var repoOK = wantRepo === '' || rowRepo === wantRepo;
      var textOK = q === '' || tr.textContent.toLowerCase().indexOf(q) !== -1;
      var show = groupOK && repoOK && textOK;
      tr.style.display = show ? '' : 'none';
      if (show) shown++;
    });
    var cnt = document.getElementById('events-count');
    if (cnt) cnt.textContent = (q || wantRepo) ? (shown + ' shown') : '';
  }
  var ebox = document.getElementById('events-search');
  if (ebox) {
    ebox.addEventListener('input', gwEventsFilter);
  }
  document.querySelectorAll('.gw-evchip').forEach(function (c) {
    c.addEventListener('click', function () {
      c.setAttribute('aria-pressed', c.getAttribute('aria-pressed') === 'false' ? 'true' : 'false');
      gwEventsFilter();
    });
  });

  // Reports page: live text filter over the rendered .report-row rows in
  // #report-out. Rows arrive via htmx, so this also runs on afterSwap. The box
  // is disabled (with a hint) until a report is loaded - you can't filter rows
  // that aren't there yet.
  function gwReportFilter() {
    var out = document.getElementById('report-out');
    var box = document.getElementById('report-filter');
    if (!out || !box) return;
    var rows = out.querySelectorAll('.report-row');
    var cnt = document.getElementById('report-filter-count');
    var hint = document.getElementById('report-filter-hint');
    if (rows.length === 0) {
      box.disabled = true;
      if (cnt) cnt.textContent = '';
      if (hint) hint.style.display = '';
      return;
    }
    box.disabled = false;
    if (hint) hint.style.display = 'none';
    var q = box.value.trim().toLowerCase();
    var shown = 0;
    rows.forEach(function (row) {
      var ok = q === '' || row.textContent.toLowerCase().indexOf(q) !== -1;
      row.style.display = ok ? '' : 'none';
      if (ok) shown++;
    });
    if (cnt) cnt.textContent = q ? (shown + ' of ' + rows.length) : (rows.length + ' rows');
  }
  var rbox = document.getElementById('report-filter');
  if (rbox) {
    rbox.addEventListener('input', gwReportFilter);
  }
  var erepoSel = document.querySelector('[data-events-repo]');
  if (erepoSel) {
    erepoSel.addEventListener('change', gwEventsFilter);
  }
  gwEventsFilter();

  var toggle = document.querySelector('[data-rail-toggle]');
  if (toggle) toggle.addEventListener('click', function () {
    var next = shell.getAttribute('data-rail') === 'expanded' ? 'collapsed' : 'expanded';
    shell.setAttribute('data-rail', next);
    localStorage.setItem(KEY, next);
  });

  var hide = document.querySelector('[data-rail-hide]');
  if (hide) hide.addEventListener('click', function () {
    shell.setAttribute('data-rail', 'hidden');
    localStorage.setItem(KEY, 'hidden');
  });

  // Mobile drawer: the top-bar hamburger opens the off-canvas rail, the backdrop
  // closes it. Transient (not persisted) so it never overrides the desktop rail
  // preference saved by the in-rail toggle above.
  var open = document.querySelector('[data-rail-open]');
  if (open) open.addEventListener('click', function () {
    shell.setAttribute('data-rail', shell.getAttribute('data-rail') === 'expanded' ? 'collapsed' : 'expanded');
  });

  var backdrop = document.querySelector('[data-rail-close]');
  if (backdrop) backdrop.addEventListener('click', function () {
    shell.setAttribute('data-rail', 'collapsed');
  });

  var sel = document.querySelector('[data-repo-switch]');
  if (sel) sel.addEventListener('change', function () {
    var u = new URL(location.href);
    if (this.value) u.searchParams.set('repo', this.value); else u.searchParams.delete('repo');
    location.assign(u.pathname + u.search);
  });

  // Settings page: each <select data-setting="KEY"> reflects + writes localStorage.
  document.querySelectorAll('[data-setting]').forEach(function (el) {
    var key = el.getAttribute('data-setting');
    var cur = localStorage.getItem(key);
    if (SET_ALLOWED[key] && SET_ALLOWED[key].indexOf(cur) === -1) cur = SET_DEFAULT[key] || '';
    if (cur) el.value = cur;
    el.addEventListener('change', function () {
      if (SET_ALLOWED[key] && SET_ALLOWED[key].indexOf(this.value) === -1) return;
      localStorage.setItem(key, this.value);
      if (key === 'gwrail') shell.setAttribute('data-rail', this.value);
      if (key === 'gwtz') { gwApplyTz(document); gwDaySeparators(); }
      if (key === 'gwtc') document.body.dataset.tc = (this.value === 'off') ? 'off' : 'on';
      if (key === 'gwday') gwDaySeparators();
    });
  });
})();
