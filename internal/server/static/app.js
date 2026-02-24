(function () {
  'use strict';

  const PAGE_SIZE = 20;

  function el(id) {
    return document.getElementById(id);
  }

  function getContent() {
    return el('content');
  }

  function pathname() {
    return window.location.pathname;
  }

  function searchParams() {
    return new URLSearchParams(window.location.search);
  }

  function apiGet(path) {
    return fetch('/api' + path, { headers: { Accept: 'application/json' } }).then(function (r) {
      if (!r.ok) throw new Error(r.status + ' ' + r.statusText);
      return r.json();
    });
  }

  function formatBytes(n) {
    if (n < 1024) return n + ' B';
    var units = ['KB', 'MB', 'GB', 'TB'];
    var u = 0;
    var v = n / 1024;
    while (v >= 1024 && u < units.length - 1) {
      v /= 1024;
      u++;
    }
    return v.toFixed(1) + ' ' + units[u];
  }

  function escapeHtml(s) {
    if (s == null) return '';
    var str = typeof s === 'string' ? s : String(s);
    var div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
  }

  var imageExts = { '.jpg': 1, '.jpeg': 1, '.png': 1, '.gif': 1, '.webp': 1, '.bmp': 1 };
  var videoExts = { '.mp4': 1, '.webm': 1, '.mkv': 1, '.mov': 1, '.avi': 1, '.m4v': 1 };
  function ext(path) {
    var i = path.lastIndexOf('.');
    return i >= 0 ? path.slice(i).toLowerCase() : '';
  }
  function isPreviewable(path) { return imageExts[ext(path)] || videoExts[ext(path)]; }
  function hasImageExt(path) { return !!imageExts[ext(path)]; }
  function hasVideoExt(path) { return !!videoExts[ext(path)]; }
  function previewURL(path) { return '/preview?path=' + encodeURIComponent(path); }

  // --- Home (/) ---
  function renderHome(summary, data) {
    var groups = data.groups || [];
    var total = data.total || 0;
    var page = Math.max(1, parseInt(searchParams().get('page') || '1', 10));
    var totalPages = total > 0 ? Math.ceil(total / PAGE_SIZE) : 1;
    if (page > totalPages) page = totalPages;
    var prevPage = page > 1 ? page - 1 : 0;
    var nextPage = page < totalPages ? page + 1 : 0;

    var summaryHtml =
      '<dl class="mt-6 grid grid-cols-1 sm:grid-cols-3 gap-4">' +
      '<div class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm">' +
      '<dt class="text-sm font-medium text-gray-500 uppercase tracking-wide">Duplicate groups</dt>' +
      '<dd class="mt-1 text-2xl font-semibold text-gray-900">' + summary.group_count + '</dd></div>' +
      '<div class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm">' +
      '<dt class="text-sm font-medium text-gray-500 uppercase tracking-wide">Duplicated files</dt>' +
      '<dd class="mt-1 text-2xl font-semibold text-gray-900">' + summary.total_files + '</dd></div>' +
      '<div class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm">' +
      '<dt class="text-sm font-medium text-gray-500 uppercase tracking-wide">Reclaimable</dt>' +
      '<dd class="mt-1 text-2xl font-bold text-blue-600">' + formatBytes(summary.reclaimable_size) + '</dd></div>' +
      '</dl>';

    var groupsHtml = '';
    if (groups.length) {
      groupsHtml =
        '<div class="mt-6 space-y-6">' +
        groups
          .map(function (g) {
            var perFile = g.file_count > 0 ? Math.floor(g.total_size / g.file_count) : 0;
            var fileList = g.files || [];
            var paths = fileList.map(function (f) { return f.path; });
            var truncated = g.file_count > fileList.length;
            var trashSvg = '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" width="20" height="20" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="m14.74 9-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 0 1-2.244 2.077H8.084a2.25 2.25 0 0 1-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 0 0-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 0 1 3.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 0 0-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 0 0-7.5 0"/></svg>';
            var pathsHtml = fileList
              .map(function (f) {
                return '<div class="py-2 px-3 rounded bg-gray-100 text-gray-700 font-mono text-sm hover:bg-gray-200 flex items-center gap-2 min-w-0">' +
                  '<span class="min-w-0 flex-1 truncate" title="' + escapeHtml(f.path) + '">' + escapeHtml(f.path) + '</span>' +
                  '<button type="button" class="file-delete" title="Delete this file (permanent)" data-file-id="' + escapeHtml(String(f.id)) + '" data-path="' + escapeHtml(f.path) + '">' + trashSvg + '</button>' +
                  '</div>';
              })
              .join('');
            var more =
              truncated
                ? '<p class="px-4 py-2 text-sm text-gray-500 border-t border-gray-200">First ' + paths.length + ' of ' + g.file_count + ' files shown.</p>'
                : '';
            var firstPath = paths[0];
            var previewHtml = '';
            if (firstPath && isPreviewable(firstPath)) {
              if (hasImageExt(firstPath)) {
                previewHtml =
                  '<div class="shrink-0 w-full sm:w-72 bg-gray-50 flex items-center justify-center p-4 min-h-[200px] sm:min-h-0">' +
                  '<a href="' + previewURL(firstPath) + '" target="_blank" rel="noopener" class="block max-w-full max-h-[280px]">' +
                  '<img src="' + previewURL(firstPath) + '" alt="" class="max-w-full max-h-[280px] object-contain rounded-lg shadow-sm" loading="lazy" />' +
                  '</a></div>';
              } else if (hasVideoExt(firstPath)) {
                previewHtml =
                  '<div class="shrink-0 w-full sm:w-72 bg-gray-50 flex items-center justify-center p-4 min-h-[200px] sm:min-h-0">' +
                  '<video src="' + previewURL(firstPath) + '" controls class="max-w-full max-h-[280px] rounded-lg shadow-sm" preload="metadata"></video>' +
                  '</div>';
              }
            }
            var arrowPathSvg =
              '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182m0-4.991v4.99"/></svg>';
            var refreshBtn =
              '<button type="button" class="group-refresh" title="Refresh group: check files on disk, mark missing as deleted, flag changed for rehash" data-hash="' + escapeHtml(g.hash) + '">' + arrowPathSvg + '</button>';
            return (
              '<section class="border border-gray-200 rounded-lg bg-white overflow-hidden shadow-sm flex flex-col sm:flex-row" data-group-hash="' + escapeHtml(g.hash) + '">' +
              '<div class="flex-1 min-w-0 border-b border-gray-200 sm:border-b-0 sm:border-r border-gray-200">' +
              '<header class="px-4 py-4">' +
              '<div class="flex items-center justify-between gap-2 flex-wrap">' +
              '<p class="text-lg font-semibold text-gray-900">' +
              g.file_count + ' file' + (g.file_count !== 1 ? 's' : '') +
              ' <span class="text-gray-500 font-normal text-base">· ' + formatBytes(perFile) + ' each</span></p>' +
              refreshBtn + '</div>' +
              '<p class="text-sm text-gray-500 mt-0.5">Group total: ' + formatBytes(g.total_size) + '</p>' +
              '</header>' +
              '<div class="px-4 py-3"><div class="space-y-1">' +
              pathsHtml +
              '</div></div>' +
              more +
              '</div>' +
              previewHtml +
              '</section>'
            );
          })
          .join('') +
        '</div>';
      if (totalPages > 1) {
        groupsHtml +=
          '<nav class="mt-6 flex items-center gap-2 flex-wrap">' +
          '<span class="text-gray-600 text-sm">Page ' +
          page +
          ' of ' +
          totalPages +
          ' (' +
          total +
          ' groups)</span>';
        if (prevPage) groupsHtml += ' <a href="/?page=' + prevPage + '" class="px-3 py-1 rounded border border-gray-300 text-gray-700 hover:bg-gray-50">Prev</a>';
        if (nextPage) groupsHtml += ' <a href="/?page=' + nextPage + '" class="px-3 py-1 rounded border border-gray-300 text-gray-700 hover:bg-gray-50">Next</a>';
        groupsHtml += '</nav>';
      }
    } else {
      groupsHtml =
        '<p class="mt-4 text-gray-500">No duplicate groups. <a href="/scans" class="text-blue-600 hover:underline">Start a scan</a> to find duplicates.</p>';
    }

    return (
      '<h1 class="text-2xl font-bold text-gray-900">Duplicate groups</h1>' +
      '<p class="mt-1 text-gray-600">Files grouped by identical content, ordered by group size. <a href="/scans" class="text-blue-600 hover:underline">Scans</a>.</p>' +
      summaryHtml +
      groupsHtml
    );
  }

  function loadHome() {
    var content = getContent();
    if (!content) return;
    var page = Math.max(1, parseInt(searchParams().get('page') || '1', 10));
    var limit = PAGE_SIZE;
    var offset = (page - 1) * PAGE_SIZE;
    Promise.all([
      apiGet('/duplicates/summary'),
      apiGet('/duplicates/groups?limit=' + limit + '&offset=' + offset + '&max_files_per_group=50'),
    ])
      .then(function (results) {
        content.innerHTML = renderHome(results[0], results[1]);
        if (!content._groupRefreshBound) {
          content._groupRefreshBound = true;
          content.addEventListener('click', onContentClick);
        }
      })
      .catch(function (err) {
        content.innerHTML = '<p class="text-red-600">Failed to load: ' + escapeHtml((err && err.message) ? err.message : 'Unknown error') + '</p>';
      });
  }

  function showDeleteError(message) {
    var content = getContent();
    if (!content) return;
    var existing = content.querySelector('#delete-error-banner');
    if (existing) existing.remove();
    if (!message) return;
    var banner = document.createElement('div');
    banner.id = 'delete-error-banner';
    banner.setAttribute('role', 'alert');
    banner.className = 'mb-4 rounded-lg border border-red-200 bg-red-50 p-4 text-red-800';
    banner.innerHTML =
      '<div class="flex items-start justify-between gap-3">' +
      '<p class="flex-1">' + escapeHtml(message) + '</p>' +
      '<button type="button" class="shrink-0 rounded px-2 py-1 text-sm font-medium text-red-700 hover:bg-red-100" data-dismiss-delete-error>Dismiss</button>' +
      '</div>';
    content.insertBefore(banner, content.firstChild);
    banner.querySelector('[data-dismiss-delete-error]').addEventListener('click', function () {
      showDeleteError(null);
    });
  }

  function getDeleteConfirmModal() {
    var overlay = document.getElementById('delete-confirm-overlay');
    if (overlay) return overlay;
    overlay = document.createElement('div');
    overlay.id = 'delete-confirm-overlay';
    overlay.setAttribute('hidden', '');
    overlay.setAttribute('role', 'dialog');
    overlay.setAttribute('aria-modal', 'true');
    overlay.setAttribute('aria-labelledby', 'delete-confirm-title');
    overlay.innerHTML =
      '<div id="delete-confirm-dialog">' +
      '<h3 id="delete-confirm-title">Permanently delete this file?</h3>' +
      '<p class="delete-confirm-path" id="delete-confirm-path"></p>' +
      '<p>It will be removed from disk. This cannot be undone.</p>' +
      '<div id="delete-confirm-log" class="delete-confirm-log" aria-live="polite"></div>' +
      '<div class="delete-confirm-actions">' +
      '<button type="button" class="delete-confirm-cancel">Cancel</button>' +
      '<button type="button" class="delete-confirm-submit">Delete</button>' +
      '</div></div>';
    document.body.appendChild(overlay);
    return overlay;
  }

  function hideDeleteConfirm() {
    var overlay = document.getElementById('delete-confirm-overlay');
    if (overlay) overlay.setAttribute('hidden', '');
  }

  function showDeleteConfirm(path, fileId) {
    var overlay = getDeleteConfirmModal();
    var pathEl = overlay.querySelector('#delete-confirm-path');
    var logEl = overlay.querySelector('#delete-confirm-log');
    var cancelBtn = overlay.querySelector('.delete-confirm-cancel');
    var submitBtn = overlay.querySelector('.delete-confirm-submit');
    if (pathEl) pathEl.textContent = path || '';
    if (logEl) {
      logEl.classList.remove('visible');
      logEl.innerHTML = '';
    }
    overlay.removeAttribute('hidden');
    cancelBtn.focus();

    function cleanup() {
      submitBtn.disabled = false;
      cancelBtn.onclick = null;
      submitBtn.onclick = null;
    }

    cancelBtn.onclick = function () {
      cleanup();
      hideDeleteConfirm();
    };

    submitBtn.onclick = function () {
      submitBtn.disabled = true;
      showDeleteError(null);
      var logEl = overlay.querySelector('#delete-confirm-log');
      logEl.classList.add('visible');
      logEl.innerHTML = '';
      function appendLogLine(text, isError) {
        var line = document.createElement('span');
        line.className = 'log-line' + (isError ? ' error' : '');
        line.textContent = text;
        logEl.appendChild(line);
        logEl.appendChild(document.createTextNode('\n'));
        logEl.scrollTop = logEl.scrollHeight;
      }

      var url = '/api/duplicates/files/delete?stream=1';
      fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Accept: 'text/plain' },
        body: JSON.stringify({ file_id: parseInt(fileId, 10) }),
      })
        .then(function (r) {
          if (!r.ok) {
            return r.text().then(function (text) {
              var msg = r.statusText;
              if (text && text.indexOf('ERROR: ') === 0) msg = text.replace(/^ERROR: /, '').trim();
              throw new Error(msg);
            });
          }
          return r.body.getReader();
        })
        .then(function (reader) {
          var decoder = new TextDecoder();
          var buffer = '';
          function handleLine(line) {
            if (line === 'DONE') {
              cleanup();
              hideDeleteConfirm();
              loadHome(); // refresh UX only after full deletion completion
              return 'done';
            }
            var isError = line.indexOf('ERROR: ') === 0;
            appendLogLine(line, isError);
            if (isError) {
              showDeleteError(line.replace(/^ERROR: /, ''));
              cleanup();
              submitBtn.disabled = false;
              cancelBtn.onclick = function () { hideDeleteConfirm(); };
              return 'error';
            }
            return null;
          }
          function read() {
            return reader.read().then(function (result) {
              if (result.done) {
                var tail = buffer.trim();
                if (tail) {
                  if (tail === 'DONE') {
                    cleanup();
                    hideDeleteConfirm();
                    loadHome(); // refresh UX only after full deletion completion
                  } else if (tail.indexOf('ERROR: ') === 0) {
                    appendLogLine(tail, true);
                    showDeleteError(tail.replace(/^ERROR: /, ''));
                    cleanup();
                    submitBtn.disabled = false;
                    cancelBtn.onclick = function () { hideDeleteConfirm(); };
                  } else {
                    appendLogLine(tail, false);
                  }
                }
                return;
              }
              buffer += decoder.decode(result.value, { stream: true });
              var lines = buffer.split('\n');
              buffer = lines.pop();
              for (var i = 0; i < lines.length; i++) {
                var line = lines[i].trim();
                if (!line) continue;
                var status = handleLine(line);
                if (status === 'done' || status === 'error') return;
              }
              return read();
            });
          }
          return read();
        })
        .catch(function (err) {
          cleanup();
          var message = (err && err.message === 'Failed to fetch')
            ? 'Delete failed: Could not reach the server. Check the server logs for details.'
            : ((err && err.message) ? err.message : 'Delete failed');
          showDeleteError(message);
          appendLogLine('ERROR: ' + message, true);
          submitBtn.disabled = false;
          cancelBtn.onclick = function () { hideDeleteConfirm(); };
        });
    };
  }

  function onContentClick(ev) {
    if (ev.target && ev.target.getAttribute && ev.target.getAttribute('data-dismiss-delete-error') !== null) return;
    var deleteBtn = ev.target && ev.target.closest && ev.target.closest('.file-delete');
    if (deleteBtn && deleteBtn.dataset && deleteBtn.dataset.fileId) {
      ev.preventDefault();
      var fileId = deleteBtn.dataset.fileId;
      var path = deleteBtn.dataset.path || '';
      showDeleteConfirm(path, fileId);
      return;
    }

    var btn = ev.target && ev.target.closest && ev.target.closest('.group-refresh');
    if (!btn || !btn.dataset || !btn.dataset.hash) return;
    ev.preventDefault();
    var hash = btn.dataset.hash;
    var section = btn.closest('section[data-group-hash]');
    btn.disabled = true;
    fetch('/api/duplicates/groups/refresh', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify({ hash: hash }),
    })
      .then(function (r) {
        if (!r.ok) return r.json().then(function (body) { throw new Error(body.error || r.statusText); });
        return r.json();
      })
      .then(function () {
        if (section) section.classList.add('opacity-75');
        loadHome();
      })
      .catch(function (err) {
        btn.disabled = false;
        btn.title = 'Error: ' + ((err && err.message) ? err.message : 'Unknown error');
      });
  }

  // --- Scans (/scans) ---
  function renderScans(roots, scans) {
    var incompleteByPath = {};
    var list = (scans || []).slice().sort(function (a, b) {
      return b.id - a.id;
    });
    list.forEach(function (s) {
      if (!s.hash_completed_at && s.root_path && incompleteByPath[s.root_path] === undefined) {
        incompleteByPath[s.root_path] = s.id;
      }
    });

    var rootsHtml =
      '<section class="mt-6">' +
      '<h2 class="text-lg font-semibold text-gray-800">Add scan root</h2>' +
      '<form action="/scans/roots" method="post" class="mt-2 flex gap-2">' +
      '<input type="text" name="path" placeholder="/path/to/dir" required class="flex-1 rounded border border-gray-300 px-3 py-2" />' +
      '<button type="submit" class="px-4 py-2 bg-gray-800 text-white rounded hover:bg-gray-900">Add</button>' +
      '</form></section>';

    rootsHtml += '<section class="mt-6"><h2 class="text-lg font-semibold text-gray-800">Scan roots</h2>';
    if (roots && roots.length) {
      rootsHtml += '<ul class="mt-2 space-y-2">';
      roots.forEach(function (r) {
        rootsHtml += '<li class="flex items-center gap-4 flex-wrap"><span class="text-gray-700">' + escapeHtml(r.path) + '</span>';
        rootsHtml +=
          '<form action="/scans/start" method="post" class="inline">' +
          '<input type="hidden" name="root_id" value="' +
          r.id +
          '" />' +
          '<button type="submit" class="px-3 py-1 text-sm bg-blue-600 text-white rounded hover:bg-blue-700">Start scan</button>' +
          '</form>';
        var incId = incompleteByPath[r.path];
        if (incId) {
          rootsHtml +=
            '<form action="/scans/' +
            incId +
            '/continue" method="post" class="inline">' +
            '<button type="submit" class="px-3 py-1 text-sm bg-amber-600 text-white rounded hover:bg-amber-700">Continue last</button>' +
            '</form>';
        }
        rootsHtml += '</li>';
      });
      rootsHtml += '</ul>';
    } else {
      rootsHtml += '<p class="mt-2 text-gray-500">No scan roots. Add one above.</p>';
    }
    rootsHtml += '</section>';

    rootsHtml += '<section class="mt-8"><h2 class="text-lg font-semibold text-gray-800">Recent scans</h2>';
    if (scans && scans.length) {
      rootsHtml +=
        '<div class="mt-2 overflow-x-auto"><table class="min-w-full border border-gray-200 rounded">' +
        '<thead class="bg-gray-50"><tr>' +
        '<th class="text-left px-4 py-2 text-gray-700">ID</th>' +
        '<th class="text-left px-4 py-2 text-gray-700">Root</th>' +
        '<th class="text-left px-4 py-2 text-gray-700">Created</th>' +
        '<th class="text-left px-4 py-2 text-gray-700">Completed</th>' +
        '<th class="text-left px-4 py-2 text-gray-700">Files</th>' +
        '<th class="text-left px-4 py-2 text-gray-700">Hash</th><th></th></tr></thead><tbody>';
      scans.forEach(function (s) {
        var created = s.created_at ? s.created_at.replace('T', ' ').slice(0, 16) : '—';
        var completed = s.completed_at ? s.completed_at.replace('T', ' ').slice(0, 16) : '—';
        var files = s.file_count != null ? s.file_count : '—';
        var hashStatus = s.hash_completed_at ? 'done' : s.hash_started_at ? 'running…' : '—';
        var continueBtn =
          !s.completed_at || !s.hash_completed_at
            ? '<form action="/scans/' +
              s.id +
              '/continue" method="post" class="inline"><button type="submit" class="text-amber-600 hover:underline text-sm">Continue</button></form>'
            : '';
        rootsHtml +=
          '<tr class="border-t border-gray-200">' +
          '<td class="px-4 py-2">' +
          s.id +
          '</td><td class="px-4 py-2 text-gray-700">' +
          escapeHtml(s.root_path) +
          '</td><td class="px-4 py-2 text-gray-600">' +
          created +
          '</td><td class="px-4 py-2 text-gray-600">' +
          completed +
          '</td><td class="px-4 py-2 text-gray-600">' +
          files +
          '</td><td class="px-4 py-2">' +
          hashStatus +
          '</td><td class="px-4 py-2 flex gap-2">' +
          '<a href="/scans/' +
          s.id +
          '" class="text-blue-600 hover:underline">Progress</a> ' +
          continueBtn +
          '</td></tr>';
      });
      rootsHtml += '</tbody></table></div>';
    } else {
      rootsHtml += '<p class="mt-2 text-gray-500">No scans yet. Start one from a root above.</p>';
    }
    rootsHtml += '</section>';

    return '<h1 class="text-2xl font-bold text-gray-900">Scans</h1>' + rootsHtml;
  }

  function loadScans() {
    var content = getContent();
    if (!content) return;
    Promise.all([apiGet('/roots'), apiGet('/scans')])
      .then(function (results) {
        content.innerHTML = renderScans(results[0], results[1]);
      })
      .catch(function (err) {
        content.innerHTML = '<p class="text-red-600">Failed to load: ' + escapeHtml((err && err.message) ? err.message : 'Unknown error') + '</p>';
      });
  }

  // --- Scan progress (/scans/:id) ---
  function renderScanProgress(scan) {
    var rootPath = scan && scan.root_path ? escapeHtml(scan.root_path) : '—';
    var scanId = scan && scan.id != null ? escapeHtml(String(scan.id)) : '';
    return (
      '<h1 class="text-2xl font-bold text-gray-900">Scan ' +
      scanId +
      '</h1>' +
      '<p class="text-gray-600 mt-1">Root: ' +
      rootPath +
      '</p>' +
      '<p class="mt-2">' +
      '<a href="/scans/' +
      scanId +
      '/export" download="scan-' +
      scanId +
      '-files.csv" class="text-blue-600 hover:underline">Download CSV (all files and hashes)</a>' +
      '</p>' +
      '<div id="scan-status" class="mt-4"><p class="text-gray-500">Loading status…</p></div>' +
      '<p class="mt-4"><a href="/scans" class="text-blue-600 hover:underline">← Back to scans</a></p>'
    );
  }

  function renderStatus(status) {
    var statusText = status.hash_completed_at ? 'Done' : status.hash_started_at ? 'Hashing…' : status.completed_at ? 'Hashing…' : 'Scanning…';
    var created = status.created_at ? status.created_at.replace('T', ' ').slice(0, 19) : '—';
    var completed = status.completed_at ? status.completed_at.replace('T', ' ').slice(0, 19) : '—';
    var hashStarted = status.hash_started_at ? status.hash_started_at.replace('T', ' ').slice(0, 19) : '—';
    var hashCompleted = status.hash_completed_at ? status.hash_completed_at.replace('T', ' ').slice(0, 19) : '—';
    var fileCount = status.file_count != null ? status.file_count : 0;
    var hashedCount = status.hashed_file_count != null ? status.hashed_file_count : '—';
    var reused = status.hash_reused_count != null ? status.hash_reused_count : '—';
    var errors = status.hash_error_count != null ? status.hash_error_count : '—';
    var statusId = status && status.id != null ? escapeHtml(String(status.id)) : '';
    var viewDup =
      status.completed_at && status.hash_completed_at
        ? '<a href="/scans/' + statusId + '/duplicates" class="text-blue-600 hover:underline">View duplicates</a>'
        : '';
    return (
      '<div class="rounded border border-gray-200 p-4 bg-white">' +
      '<table class="min-w-full text-sm">' +
      '<tr><td class="font-medium text-gray-700 pr-4">Status</td><td>' +
      escapeHtml(statusText) +
      '</td></tr>' +
      '<tr><td class="font-medium text-gray-700 pr-4">Created</td><td>' +
      escapeHtml(created) +
      '</td></tr>' +
      '<tr><td class="font-medium text-gray-700 pr-4">Completed</td><td>' +
      escapeHtml(completed) +
      '</td></tr>' +
      '<tr><td class="font-medium text-gray-700 pr-4">Files scanned</td><td>' +
      escapeHtml(String(fileCount)) +
      '</td></tr>' +
      '<tr><td class="font-medium text-gray-700 pr-4">Hash started</td><td>' +
      escapeHtml(String(hashStarted)) +
      '</td></tr>' +
      '<tr><td class="font-medium text-gray-700 pr-4">Hash completed</td><td>' +
      escapeHtml(String(hashCompleted)) +
      '</td></tr>' +
      '<tr><td class="font-medium text-gray-700 pr-4">Files with hash</td><td>' +
      escapeHtml(String(hashedCount)) +
      '</td></tr>' +
      '<tr><td class="font-medium text-gray-700 pr-4">Reused (no read)</td><td>' +
      escapeHtml(String(reused)) +
      '</td></tr>' +
      '<tr><td class="font-medium text-gray-700 pr-4">Hash errors</td><td>' +
      escapeHtml(String(errors)) +
      '</td></tr>' +
      '</table>' +
      '<p class="mt-2 flex gap-4">' +
      '<a href="/scans/' +
      statusId +
      '/export" download="scan-' +
      statusId +
      '-files.csv" class="text-blue-600 hover:underline">Download CSV</a> ' +
      viewDup +
      '</p></div>'
    );
  }

  function loadScanProgress(scanId) {
    var content = getContent();
    if (!content) return;
    apiGet('/scans/' + scanId)
      .then(function (scan) {
        content.innerHTML = renderScanProgress(scan);
        var statusEl = el('scan-status');
        if (!statusEl) return;

        function poll() {
          apiGet('/scans/' + scanId + '/status')
            .then(function (status) {
              statusEl.innerHTML = renderStatus(status);
              if (!status.hash_completed_at) {
                setTimeout(poll, 2000);
              }
            })
            .catch(function (err) {
              statusEl.innerHTML = '<p class="text-red-600">Failed to load status: ' + escapeHtml((err && err.message) ? err.message : 'Unknown error') + '</p>';
            });
        }
        poll();
      })
      .catch(function (err) {
        content.innerHTML = '<p class="text-red-600">Failed to load scan: ' + escapeHtml((err && err.message) ? err.message : 'Unknown error') + '</p>';
      });
  }

  // --- Admin (/admin) ---
  function renderAdmin() {
    return (
      '<h1 class="text-2xl font-bold text-gray-900">Admin</h1>' +
      '<p class="mt-1 text-gray-600">Maintenance and repair actions.</p>' +
      '<section class="mt-6 rounded-lg border border-gray-200 bg-white p-6 shadow-sm">' +
      '<h2 class="text-lg font-semibold text-gray-800">Duplicate groups</h2>' +
      '<p class="mt-1 text-gray-600">Rebuild the duplicate groups table from all hashed files. Use if the list looks wrong or after restoring data.</p>' +
      '<button type="button" id="admin-refresh-groups" class="mt-4 inline-flex items-center gap-2 px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed">Refresh all groups<svg class="w-4 h-4 shrink-0" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182m0-4.991v4.99"/></svg></button>' +
      '<span id="admin-refresh-status" class="ml-3 text-sm text-gray-500"></span>' +
      '</section>'
    );
  }

  function loadAdmin() {
    var content = getContent();
    if (!content) return;
    content.innerHTML = renderAdmin();
    var btn = document.getElementById('admin-refresh-groups');
    var statusEl = document.getElementById('admin-refresh-status');
    if (btn && statusEl) {
      btn.addEventListener('click', function () {
        btn.disabled = true;
        statusEl.textContent = 'Refreshing…';
        statusEl.classList.remove('text-red-600', 'text-green-600');
        statusEl.classList.add('text-gray-500');
        fetch('/api/admin/refresh-duplicate-groups', { method: 'POST', headers: { Accept: 'application/json' } })
          .then(function (r) {
            if (!r.ok) return r.json().then(function (body) { throw new Error(body.error || r.statusText); });
            return r.json();
          })
          .then(function () {
            statusEl.textContent = 'Done.';
            statusEl.classList.remove('text-gray-500');
            statusEl.classList.add('text-green-600');
          })
          .catch(function (err) {
            var msg = (err && err.message) ? String(err.message) : 'Request failed';
            statusEl.textContent = 'Failed: ' + msg;
            statusEl.classList.remove('text-gray-500');
            statusEl.classList.add('text-red-600');
          })
          .finally(function () {
            btn.disabled = false;
          });
      });
    }
  }

  // --- Router ---
  function run() {
    var content = getContent();
    if (!content) return;

    var path = pathname();
    if (path === '/' || path === '') {
      loadHome();
      return;
    }
    if (path === '/scans') {
      loadScans();
      return;
    }
    if (path === '/admin') {
      loadAdmin();
      return;
    }
    var scanMatch = /^\/scans\/(\d+)$/.exec(path);
    if (scanMatch) {
      var scanId = scanMatch[1];
      if (content.getAttribute('data-scan-id') === scanId) {
        loadScanProgress(scanId);
      }
    }
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', run);
  } else {
    run();
  }
})();
