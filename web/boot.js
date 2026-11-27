// Apply the language before paint; boot-theme.js owns the theme.
// i18n.js replaces lfT. Boot must still render if that script fails to load.
// Visible UI is Vietnamese by default, or English. A leftover Chinese
// localStorage value is migrated so a hard refresh no longer stays on Chinese.
window.lfT = window.lfT || ((zh) => zh);
(() => {
  const root = document.documentElement;
  let lang = 'vi';
  try {
    const saved = localStorage.getItem('loopforge.lang');
    if (saved === 'en') lang = 'en';
    else {
      lang = 'vi';
      if (saved && saved !== 'vi') {
        try { localStorage.setItem('loopforge.lang', 'vi'); } catch (_) { /* ignore */ }
      }
    }
  } catch (_) { /* Restricted storage still permits the default interface. */ }
  root.dataset.lang = lang;
  root.lang = lang;
  try {
    if (sessionStorage.getItem('loopforge.lang.motion') === 'in') {
      if (matchMedia('(prefers-reduced-motion: reduce)').matches) sessionStorage.removeItem('loopforge.lang.motion');
      else root.classList.add('lang-enter');
    }
  } catch (_) { /* Enter animation is optional when storage is blocked. */ }
  document.write('<script src="/i18n-' + lang + '.js"><\/script>');
})();
