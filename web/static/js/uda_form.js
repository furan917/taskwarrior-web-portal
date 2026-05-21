// uda_form.js - show/hide Values field and update the type hint when the
// type select changes. Hooks via htmx:afterSwap (CSP: no unsafe-inline).
(function () {
  var hints = {
    string:   'Free text value. Optionally restrict to a fixed list using the Values field below.',
    numeric:  'A number, e.g. 3 or 1.5.',
    date:     'A date, e.g. 2026-06-01 or due:tomorrow.',
    duration: 'A time span, e.g. 2h or 30min.',
  };

  function init(root) {
    var sel = root.querySelector('#uda-type-select');
    if (!sel) return;
    var section = root.querySelector('#uda-values-section');
    var hint = root.querySelector('#uda-type-hint');

    function update() {
      if (section) section.style.display = sel.value === 'string' ? '' : 'none';
      if (hint) hint.textContent = hints[sel.value] || '';
    }

    update();
    sel.addEventListener('change', update);
  }

  document.body.addEventListener('htmx:afterSwap', function (evt) {
    var target = evt.detail && evt.detail.target;
    if (!target) return;
    var dialog = target.closest('dialog') || target;
    init(dialog);
  });
})();
