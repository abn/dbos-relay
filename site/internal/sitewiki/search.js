(function () {
  'use strict';

  var searchIndex = null;
  var isLoading = false;
  var selectedIndex = -1;
  var currentResults = [];
  var lastActiveElement = null;

  var searchBtn = document.getElementById('searchBtn');
  var mobileSearchBtn = document.getElementById('mobileSearchBtn');
  var backdrop = document.getElementById('searchBackdrop');
  var modal = document.querySelector('.search-modal');
  var input = document.getElementById('searchInput');
  var closeBtn = document.getElementById('searchCloseBtn');
  var resultsContainer = document.getElementById('searchResults');

  if (!backdrop || !input || !resultsContainer) {
    return;
  }

  function fetchIndex() {
    if (searchIndex || isLoading) {
      return Promise.resolve(searchIndex);
    }
    isLoading = true;
    return fetch('/wiki/search-index.json')
      .then(function (res) {
        if (!res.ok) {
          throw new Error('HTTP ' + res.status);
        }
        return res.json();
      })
      .then(function (data) {
        searchIndex = data;
        isLoading = false;
        return searchIndex;
      })
      .catch(function (err) {
        isLoading = false;
        console.warn('Failed to load wiki search index:', err);
        return [];
      });
  }

  function openSearch() {
    lastActiveElement = document.activeElement;
    backdrop.classList.add('open');
    backdrop.setAttribute('aria-hidden', 'false');
    document.body.style.overflow = 'hidden';
    fetchIndex().then(function () {
      if (input.value.trim().length > 0) {
        runSearch();
      }
    });
    input.value = '';
    selectedIndex = -1;
    currentResults = [];
    renderInitialState();
    setTimeout(function () {
      input.focus();
    }, 20);
  }

  function closeSearch() {
    if (!backdrop.classList.contains('open')) {
      return;
    }
    backdrop.classList.remove('open');
    backdrop.setAttribute('aria-hidden', 'true');
    document.body.style.overflow = '';
    if (lastActiveElement && typeof lastActiveElement.focus === 'function') {
      lastActiveElement.focus();
    }
  }

  function escapeHTML(str) {
    if (!str) return '';
    return str
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#039;');
  }

  function escapeRegExp(str) {
    return str.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  }

  function highlight(text, terms) {
    if (!text || !terms || terms.length === 0) {
      return escapeHTML(text);
    }
    var escaped = escapeHTML(text);
    var pattern = terms.map(function (t) { return escapeRegExp(escapeHTML(t)); }).join('|');
    if (!pattern) return escaped;
    var regex = new RegExp('(' + pattern + ')', 'gi');
    return escaped.replace(regex, '<mark class="search-match">$1</mark>');
  }

  function extractSnippet(content, terms) {
    if (!content) return '';
    var lower = content.toLowerCase();
    var firstPos = -1;

    for (var i = 0; i < terms.length; i++) {
      var pos = lower.indexOf(terms[i]);
      if (pos !== -1 && (firstPos === -1 || pos < firstPos)) {
        firstPos = pos;
      }
    }

    if (firstPos === -1) {
      var snippet = content.slice(0, 110);
      return highlight(snippet + (content.length > 110 ? '...' : ''), terms);
    }

    var start = Math.max(0, firstPos - 35);
    var end = Math.min(content.length, firstPos + 95);
    var prefix = start > 0 ? '...' : '';
    var suffix = end < content.length ? '...' : '';

    return prefix + highlight(content.slice(start, end), terms) + suffix;
  }

  function renderInitialState() {
    resultsContainer.innerHTML =
      '<div class="search-empty-state">' +
      '<p>Type a search term or browse documentation topics...</p>' +
      '<div class="search-quick-links">' +
      '<span>Popular:</span>' +
      '<a href="/wiki/usage/quickstart.html">Quickstart</a>' +
      '<a href="/wiki/protocol/executor-ws.html">Protocol Spec</a>' +
      '<a href="/wiki/architecture/recovery.html">Lease Fencing</a>' +
      '<a href="/wiki/adr/0011-dual-engine-storage-architecture.html">Dual Engine</a>' +
      '</div>' +
      '</div>';
  }

  function runSearch() {
    var query = input.value.trim().toLowerCase();
    if (!query) {
      selectedIndex = -1;
      currentResults = [];
      renderInitialState();
      return;
    }

    if (!searchIndex) {
      resultsContainer.innerHTML =
        '<div class="search-empty-state"><p>Loading search index...</p></div>';
      fetchIndex().then(function () {
        runSearch();
      });
      return;
    }

    var terms = query.split(/\s+/).filter(Boolean);
    if (terms.length === 0) {
      renderInitialState();
      return;
    }

    var matches = [];

    for (var i = 0; i < searchIndex.length; i++) {
      var entry = searchIndex[i];
      var titleLower = entry.t ? entry.t.toLowerCase() : '';
      var docLower = entry.d ? entry.d.toLowerCase() : '';
      var secLower = entry.s ? entry.s.toLowerCase() : '';
      var contentLower = entry.c ? entry.c.toLowerCase() : '';

      var allMatch = true;
      var score = 0;

      for (var j = 0; j < terms.length; j++) {
        var term = terms[j];
        var termMatched = false;

        if (titleLower.indexOf(term) !== -1) {
          termMatched = true;
          score += 45;
          if (titleLower === term || titleLower.startsWith(term + ' ')) {
            score += 40;
          }
        }
        if (docLower.indexOf(term) !== -1) {
          termMatched = true;
          score += 25;
        }
        if (secLower.indexOf(term) !== -1) {
          termMatched = true;
          score += 15;
        }
        if (contentLower.indexOf(term) !== -1) {
          termMatched = true;
          score += 10;
        }

        if (!termMatched) {
          allMatch = false;
          break;
        }
      }

      if (allMatch) {
        if (titleLower.indexOf(query) !== -1) {
          score += 80;
        } else if (docLower.indexOf(query) !== -1) {
          score += 40;
        } else if (contentLower.indexOf(query) !== -1) {
          score += 30;
        }

        matches.push({
          entry: entry,
          score: score
        });
      }
    }

    matches.sort(function (a, b) {
      return b.score - a.score;
    });

    currentResults = matches.slice(0, 12).map(function (m) {
      return m.entry;
    });

    renderResults(terms, query);
  }

  function renderResults(terms, query) {
    if (currentResults.length === 0) {
      selectedIndex = -1;
      resultsContainer.innerHTML =
        '<div class="search-empty-state">' +
        '<p>No results found for "<b>' + escapeHTML(query) + '</b>"</p>' +
        '<p class="search-empty-hint">Try searching for keywords like <code>quickstart</code>, <code>fencing</code>, <code>websocket</code>, or <code>storage</code>.</p>' +
        '</div>';
      return;
    }

    selectedIndex = 0;
    var html = '<div class="search-results-list" role="listbox">';

    for (var i = 0; i < currentResults.length; i++) {
      var item = currentResults[i];
      var isSelected = i === selectedIndex;
      var snippet = extractSnippet(item.c, terms);
      var titleHtml = highlight(item.t || item.d, terms);

      html +=
        '<a href="' + escapeHTML(item.u) + '" class="search-result-item' + (isSelected ? ' selected' : '') + '" role="option" aria-selected="' + isSelected + '" data-index="' + i + '">' +
        '  <div class="search-result-top">' +
        '    <span class="search-result-section">' + escapeHTML(item.s) + '</span>' +
        '    <span class="search-result-sep">/</span>' +
        '    <span class="search-result-doc">' + escapeHTML(item.d) + '</span>' +
        '  </div>' +
        '  <div class="search-result-title">' +
        '    <svg class="search-result-icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>' +
        '    <span>' + titleHtml + '</span>' +
        '  </div>' +
        (snippet ? '  <div class="search-result-snippet">' + snippet + '</div>' : '') +
        '</a>';
    }

    html += '</div>';
    resultsContainer.innerHTML = html;
  }

  function updateSelection() {
    var items = resultsContainer.querySelectorAll('.search-result-item');
    for (var i = 0; i < items.length; i++) {
      if (i === selectedIndex) {
        items[i].classList.add('selected');
        items[i].setAttribute('aria-selected', 'true');
        items[i].scrollIntoView({ block: 'nearest' });
      } else {
        items[i].classList.remove('selected');
        items[i].setAttribute('aria-selected', 'false');
      }
    }
  }

  function moveSelection(delta) {
    if (currentResults.length === 0) return;
    selectedIndex += delta;
    if (selectedIndex < 0) {
      selectedIndex = currentResults.length - 1;
    } else if (selectedIndex >= currentResults.length) {
      selectedIndex = 0;
    }
    updateSelection();
  }

  function openSelected() {
    if (selectedIndex >= 0 && selectedIndex < currentResults.length) {
      var item = currentResults[selectedIndex];
      window.location.href = item.u;
    }
  }

  function isEditable(el) {
    if (!el) return false;
    var tag = el.tagName ? el.tagName.toLowerCase() : '';
    return tag === 'input' || tag === 'textarea' || tag === 'select' || el.isContentEditable;
  }

  // Event Listeners
  if (searchBtn) {
    searchBtn.addEventListener('click', openSearch);
    searchBtn.addEventListener('mouseenter', fetchIndex, { once: true });
    searchBtn.addEventListener('focus', fetchIndex, { once: true });
  }

  if (mobileSearchBtn) {
    mobileSearchBtn.addEventListener('click', openSearch);
    mobileSearchBtn.addEventListener('mouseenter', fetchIndex, { once: true });
    mobileSearchBtn.addEventListener('focus', fetchIndex, { once: true });
  }

  if (closeBtn) {
    closeBtn.addEventListener('click', closeSearch);
  }

  backdrop.addEventListener('click', function (e) {
    if (e.target === backdrop) {
      closeSearch();
    }
  });

  input.addEventListener('input', runSearch);

  input.addEventListener('keydown', function (e) {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      moveSelection(1);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      moveSelection(-1);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      openSelected();
    } else if (e.key === 'Escape') {
      e.preventDefault();
      closeSearch();
    }
  });

  document.addEventListener('keydown', function (e) {
    if ((e.key === 'k' || e.key === 'K') && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      if (backdrop.classList.contains('open')) {
        closeSearch();
      } else {
        openSearch();
      }
      return;
    }
    if (e.key === '/' && !backdrop.classList.contains('open') && !isEditable(document.activeElement)) {
      e.preventDefault();
      openSearch();
      return;
    }
    if (e.key === 'Escape' && backdrop.classList.contains('open')) {
      closeSearch();
    }
  });

  // Hovering on result items updates selectedIndex
  resultsContainer.addEventListener('mousemove', function (e) {
    var item = e.target.closest('.search-result-item');
    if (item && item.dataset.index) {
      var idx = parseInt(item.dataset.index, 10);
      if (idx !== selectedIndex) {
        selectedIndex = idx;
        updateSelection();
      }
    }
  });
})();
