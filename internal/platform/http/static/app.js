// Satırın tamamı tıklanabilir listeler: <tr data-href="/..."> — içindeki bağlantı/düğme/form kendi işini yapar.
document.addEventListener('click', function (e) {
  var row = e.target.closest('tr[data-href]');
  if (!row || e.target.closest('a, button, input, select, textarea, label, form')) return;
  if (e.ctrlKey || e.metaKey) {
    window.open(row.dataset.href, '_blank', 'noopener');
  } else {
    window.location.href = row.dataset.href;
  }
});
