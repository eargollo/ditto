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
    var div = document.createElement('div');
    div.textContent = s;
    return div.innerHTML;
  }

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
      '<div class="mt-4 p-4 rounded-lg bg-gray-50 border border-gray-200">' +
      '<p class="text-gray-800 font-medium">Summary</p>' +
      '<p class="mt-1 text-gray-600">' +
      summary.group_count +
      ' duplicate group' +
      (summary.group_count !== 1 ? 's' : '') +
      ' · ' +
      summary.total_files +
      ' duplicated file' +
      (summary.total_files !== 1 ? 's' : '') +
      ' · ' +
      formatBytes(summary.reclaimable_size) +
      ' can be saved</p></div>';

    var groupsHtml = '';
    if (groups.length) {
      groupsHtml =
        '<div class="mt-6 space-y-6">' +
        groups
          .map(function (g) {
            var perFile = g.file_count > 0 ? Math.floor(g.total_size / g.file_count) : 0;
            var paths = (g.files || []).map(function (f) {
              return f.path;
            });
            var truncated = g.file_count > paths.length;
            var pathsHtml = paths
              .map(function (p) {
                return '<div class="py-2 px-3 rounded bg-gray-100 text-gray-700 font-mono text-sm break-all hover:bg-gray-200">' + escapeHtml(p) + '</div>';
              })
              .join('');
            var more =
              truncated
                ? '<p class="px-4 py-2 text-sm text-gray-500 border-t border-gray-200">First ' +
                  paths.length +
                  ' shown. <a href="/duplicates/hash/' +
                  escapeHtml(g.hash) +
                  '" class="text-blue-600 hover:underline">View all ' +
                  g.file_count +
                  ' files</a></p>'
                : '';
            return (
              '<section class="border border-gray-200 rounded-lg bg-white overflow-hidden">' +
              '<div class="px-4 py-3 bg-gray-50 border-b border-gray-200 flex flex-wrap items-center gap-4">' +
              '<span class="font-semibold text-gray-800">' +
              g.file_count +
              ' file' +
              (g.file_count !== 1 ? 's' : '') +
              ' · ' +
              formatBytes(perFile) +
              ' each · ' +
              formatBytes(g.total_size) +
              ' group total</span> ' +
              '<a href="/duplicates/hash/' +
              escapeHtml(g.hash) +
              '" class="text-sm text-blue-600 hover:underline">View group details</a>' +
              '</div>' +
              '<div class="px-4 py-3"><div class="space-y-1">' +
              pathsHtml +
              '</div></div>' +
              more +
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
      apiGet('/duplicates/groups?limit=' + limit + '&offset=' + offset),
    ])
      .then(function (results) {
        content.innerHTML = renderHome(results[0], results[1]);
      })
      .catch(function (err) {
        content.innerHTML = '<p class="text-red-600">Failed to load: ' + escapeHtml(err.message) + '</p>';
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
        content.innerHTML = '<p class="text-red-600">Failed to load: ' + escapeHtml(err.message) + '</p>';
      });
  }

  // --- Scan progress (/scans/:id) ---
  function renderScanProgress(scan) {
    var rootPath = scan && scan.root_path ? escapeHtml(scan.root_path) : '—';
    return (
      '<h1 class="text-2xl font-bold text-gray-900">Scan ' +
      scan.id +
      '</h1>' +
      '<p class="text-gray-600 mt-1">Root: ' +
      rootPath +
      '</p>' +
      '<p class="mt-2">' +
      '<a href="/scans/' +
      scan.id +
      '/export" download="scan-' +
      scan.id +
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
    var viewDup =
      status.completed_at && status.hash_completed_at
        ? '<a href="/scans/' + status.id + '/duplicates" class="text-blue-600 hover:underline">View duplicates</a>'
        : '';
    return (
      '<div class="rounded border border-gray-200 p-4 bg-white">' +
      '<table class="min-w-full text-sm">' +
      '<tr><td class="font-medium text-gray-700 pr-4">Status</td><td>' +
      statusText +
      '</td></tr>' +
      '<tr><td class="font-medium text-gray-700 pr-4">Created</td><td>' +
      created +
      '</td></tr>' +
      '<tr><td class="font-medium text-gray-700 pr-4">Completed</td><td>' +
      completed +
      '</td></tr>' +
      '<tr><td class="font-medium text-gray-700 pr-4">Files scanned</td><td>' +
      fileCount +
      '</td></tr>' +
      '<tr><td class="font-medium text-gray-700 pr-4">Hash started</td><td>' +
      hashStarted +
      '</td></tr>' +
      '<tr><td class="font-medium text-gray-700 pr-4">Hash completed</td><td>' +
      hashCompleted +
      '</td></tr>' +
      '<tr><td class="font-medium text-gray-700 pr-4">Files with hash</td><td>' +
      hashedCount +
      '</td></tr>' +
      '<tr><td class="font-medium text-gray-700 pr-4">Reused (no read)</td><td>' +
      reused +
      '</td></tr>' +
      '<tr><td class="font-medium text-gray-700 pr-4">Hash errors</td><td>' +
      errors +
      '</td></tr>' +
      '</table>' +
      '<p class="mt-2 flex gap-4">' +
      '<a href="/scans/' +
      status.id +
      '/export" download="scan-' +
      status.id +
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
              statusEl.innerHTML = '<p class="text-red-600">Failed to load status: ' + escapeHtml(err.message) + '</p>';
            });
        }
        poll();
      })
      .catch(function (err) {
        content.innerHTML = '<p class="text-red-600">Failed to load scan: ' + escapeHtml(err.message) + '</p>';
      });
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
