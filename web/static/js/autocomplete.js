// autocomplete.js - themed autocomplete dropdown replacing native <datalist>.
// Generic over project / tags / deps via the data-ac-mode attribute. Single
// IIFE handles open/close/filter/keyboard nav/selection; consumers (deps.js)
// hook in via the autocomplete:select custom event.
// ─── Themed autocomplete (v5) ───────────────────────────────────────────
// Replacement for native <datalist>: a styled dropdown we fully control in
// light + dark mode, with consistent positioning and keyboard nav. Each
// instance is identified by a wrapping element with [data-ac]; inside it
// expects:
//   - exactly one [data-ac-input]   (the typed-into <input>)
//   - exactly one [data-ac-list]    (the <ul> of options)
//   - zero or more [role=option] inside the list, each carrying:
//       data-ac-value  - what gets used on selection
//       data-ac-text   - what gets searched and displayed
//
// Selection mode (read off [data-ac-mode]):
//   - "single"  - input.value = picked value (default).
//   - "tokens"  - replace the LAST comma-separated token in input.value
//                  with the picked value (used by the comma-list Tags field).
//   - "deps"    - dispatch only - the dep-picker IIFE below intercepts the
//                  autocomplete:select event, preventDefaults the value
//                  mutation, and adds a pill instead.
//
// Wired via document delegation so any number of htmx modal swaps work.
(function () {
  let openPicker = null;

  function pickerOf(el) {
    return el && el.closest && el.closest('[data-ac]');
  }
  function inputOf(picker) {
    return picker && picker.querySelector('[data-ac-input]');
  }
  function listOf(picker) {
    return picker && picker.querySelector('[data-ac-list]');
  }
  function items(picker) {
    return Array.from(picker.querySelectorAll('[data-ac-list] [role="option"]'));
  }
  function visibleItems(picker) {
    return items(picker).filter(function (li) {
      return !li.classList.contains('hidden');
    });
  }
  function clearHighlight(picker) {
    items(picker).forEach(function (li) { li.removeAttribute('aria-selected'); });
  }
  function setHighlight(picker, idx) {
    const vis = visibleItems(picker);
    if (vis.length === 0) return;
    if (idx < 0) idx = vis.length - 1;
    if (idx >= vis.length) idx = 0;
    clearHighlight(picker);
    vis[idx].setAttribute('aria-selected', 'true');
    vis[idx].scrollIntoView({ block: 'nearest' });
  }
  function highlightedIndex(picker) {
    const vis = visibleItems(picker);
    for (let i = 0; i < vis.length; i++) {
      if (vis[i].getAttribute('aria-selected') === 'true') return i;
    }
    return -1;
  }
  function open(picker) {
    const list = listOf(picker);
    if (!list) return;
    const inp = inputOf(picker);
    if (inp) {
      const r = inp.getBoundingClientRect();
      list.style.cssText = 'position:fixed;top:' + (r.bottom + 2) + 'px;left:' + r.left + 'px;width:' + r.width + 'px;right:auto;z-index:9999;';
    }
    list.classList.remove('hidden');
    if (openPicker && openPicker !== picker) close(openPicker);
    openPicker = picker;
  }
  function close(picker) {
    const list = listOf(picker);
    if (list) {
      list.classList.add('hidden');
      list.style.cssText = '';
    }
    clearHighlight(picker);
    if (openPicker === picker) openPicker = null;
  }
  function currentToken(input, mode) {
    if (mode === 'tokens') {
      const at = input.value.lastIndexOf(',');
      return input.value.substring(at + 1).trim();
    }
    return input.value;
  }
  function filter(picker, query) {
    const q = (query || '').toLowerCase().trim();
    let firstVisibleVisIdx = -1;
    let visIdx = -1;
    items(picker).forEach(function (li) {
      const t = (li.getAttribute('data-ac-text') || li.getAttribute('data-ac-value') || '').toLowerCase();
      const match = q === '' || t.indexOf(q) !== -1;
      li.classList.toggle('hidden', !match);
      if (match) {
        visIdx++;
        if (firstVisibleVisIdx === -1) firstVisibleVisIdx = visIdx;
      }
    });
    clearHighlight(picker);
    // Auto-highlight first match only when the user has actually typed
    // something - otherwise the dropdown opening with a pre-selected first
    // row feels presumptuous.
    if (q !== '' && firstVisibleVisIdx >= 0) {
      setHighlight(picker, firstVisibleVisIdx);
    }
  }
  function pick(input, item) {
    const value = item.getAttribute('data-ac-value');
    const text = item.getAttribute('data-ac-text') || value;
    const evt = new CustomEvent('autocomplete:select', {
      detail: { value: value, text: text },
      bubbles: true,
      cancelable: true,
    });
    const accepted = input.dispatchEvent(evt);
    const picker = pickerOf(input);
    const mode = (picker && picker.getAttribute('data-ac-mode')) || 'single';
    if (accepted) {
      if (mode === 'tokens') {
        const at = input.value.lastIndexOf(',');
        if (at === -1) {
          input.value = value;
        } else {
          input.value = input.value.substring(0, at + 1) + ' ' + value;
        }
      } else if (mode === 'single') {
        input.value = value;
      }
      // mode === 'deps': default behaviour is no-op; consumer handles it.
    }
    if (picker) close(picker);
  }

  document.addEventListener('focusin', function (e) {
    const inp = e.target.closest && e.target.closest('[data-ac-input]');
    if (!inp) return;
    const picker = pickerOf(inp);
    if (!picker) return;
    filter(picker, currentToken(inp, picker.getAttribute('data-ac-mode')));
    open(picker);
  });
  document.addEventListener('input', function (e) {
    const inp = e.target.closest && e.target.closest('[data-ac-input]');
    if (!inp) return;
    const picker = pickerOf(inp);
    if (!picker) return;
    filter(picker, currentToken(inp, picker.getAttribute('data-ac-mode')));
    open(picker);
  });
  document.addEventListener('keydown', function (e) {
    const inp = e.target.closest && e.target.closest('[data-ac-input]');
    if (!inp) return;
    const picker = pickerOf(inp);
    if (!picker) return;
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      open(picker);
      const cur = highlightedIndex(picker);
      setHighlight(picker, cur + 1);
      return;
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault();
      open(picker);
      const cur = highlightedIndex(picker);
      setHighlight(picker, cur < 0 ? -1 : cur - 1);
      return;
    }
    if (e.key === 'Escape') {
      close(picker);
      return;
    }
    if (e.key === 'Enter') {
      const idx = highlightedIndex(picker);
      const vis = visibleItems(picker);
      if (idx >= 0 && vis[idx]) {
        e.preventDefault();
        pick(inp, vis[idx]);
      }
      // No highlighted item: defer to consumers (dep-picker has its own
      // resolveTypedToOption fallback) or let the form submit.
    }
  });
  // mousedown (not click) so the input doesn't blur and close the list
  // before the selection registers.
  document.addEventListener('mousedown', function (e) {
    const item = e.target.closest && e.target.closest('[data-ac-list] [role="option"]');
    if (!item) return;
    const picker = pickerOf(item);
    const inp = inputOf(picker);
    if (!inp) return;
    e.preventDefault();
    pick(inp, item);
    inp.focus();
  });
  // Click outside any open picker closes it. focusout is unreliable because
  // mousedown on a list item runs before blur and we already preventDefault
  // there - this handles the "user clicked elsewhere on the page" case.
  document.addEventListener('mousedown', function (e) {
    if (!openPicker) return;
    if (openPicker.contains(e.target)) return;
    close(openPicker);
  });
})();

