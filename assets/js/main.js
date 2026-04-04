// Minimal JS — smooth scroll, active nav highlight
document.addEventListener('DOMContentLoaded', () => {
  const current = window.location.pathname;
  document.querySelectorAll('.nav-links a').forEach(link => {
    if (link.getAttribute('href') && current.endsWith(link.getAttribute('href'))) {
      link.style.color = 'var(--accent)';
    }
  });
});
